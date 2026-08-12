use fs2::FileExt;
use rand::{rngs::OsRng, RngCore};
use serde::{Deserialize, Serialize};
use serde_json::json;
use std::{
    fs::{self, File, OpenOptions},
    io::{BufRead, BufReader, Write},
    net::{SocketAddr, TcpListener, TcpStream},
    path::{Path, PathBuf},
    sync::{
        atomic::{AtomicBool, Ordering},
        Arc, Mutex,
    },
    thread,
    time::Duration,
};
use tauri::{AppHandle, Manager};

#[derive(Debug, Serialize, Deserialize)]
struct InstanceMetadata {
    schema: String,
    pid: u32,
    wake_port: u16,
    wake_secret: String,
}

pub enum InstanceAcquire {
    Primary(DesktopInstance),
    SecondaryWoke,
}

pub struct DesktopInstance {
    _lock_file: File,
    metadata_path: PathBuf,
    listener: Mutex<Option<TcpListener>>,
    stop: Arc<AtomicBool>,
    wake_secret: String,
}

impl DesktopInstance {
    pub fn acquire(data_root: &Path) -> Result<InstanceAcquire, String> {
        let state_root = data_root.join("state");
        fs::create_dir_all(&state_root)
            .map_err(|error| format!("create desktop state directory: {error}"))?;
        let lock_path = state_root.join("desktop.instance.lock");
        let metadata_path = state_root.join("desktop.instance.json");
        let lock_file = OpenOptions::new()
            .create(true)
            .read(true)
            .write(true)
            .open(&lock_path)
            .map_err(|error| format!("open desktop instance lock: {error}"))?;

        match lock_file.try_lock_exclusive() {
            Ok(()) => {
                let listener = TcpListener::bind(("127.0.0.1", 0))
                    .map_err(|error| format!("bind desktop wake listener: {error}"))?;
                listener
                    .set_nonblocking(true)
                    .map_err(|error| format!("configure desktop wake listener: {error}"))?;
                let wake_port = listener
                    .local_addr()
                    .map_err(|error| format!("read desktop wake address: {error}"))?
                    .port();
                let wake_secret = random_secret();
                let metadata = InstanceMetadata {
                    schema: "cwapi.desktop.instance.v1".into(),
                    pid: std::process::id(),
                    wake_port,
                    wake_secret: wake_secret.clone(),
                };
                write_metadata(&metadata_path, &metadata)?;
                Ok(InstanceAcquire::Primary(Self {
                    _lock_file: lock_file,
                    metadata_path,
                    listener: Mutex::new(Some(listener)),
                    stop: Arc::new(AtomicBool::new(false)),
                    wake_secret,
                }))
            }
            Err(_) => {
                wake_existing(&metadata_path)?;
                Ok(InstanceAcquire::SecondaryWoke)
            }
        }
    }

    pub fn start_listener(&self, app: AppHandle) -> Result<(), String> {
        let listener = self
            .listener
            .lock()
            .map_err(|_| "desktop wake listener state poisoned".to_string())?
            .take()
            .ok_or_else(|| "desktop wake listener already started".to_string())?;
        let stop = Arc::clone(&self.stop);
        let expected_secret = self.wake_secret.clone();
        thread::Builder::new()
            .name("cwapi-desktop-wake".into())
            .spawn(move || {
                while !stop.load(Ordering::Relaxed) {
                    match listener.accept() {
                        Ok((stream, _)) => {
                            if authenticate_wake(stream, &expected_secret) {
                                show_main_window(&app);
                            }
                        }
                        Err(error) if error.kind() == std::io::ErrorKind::WouldBlock => {
                            thread::sleep(Duration::from_millis(80));
                        }
                        Err(_) => break,
                    }
                }
            })
            .map_err(|error| format!("start desktop wake listener: {error}"))?;
        Ok(())
    }

    pub fn prepare_shutdown(&self) {
        self.stop.store(true, Ordering::Relaxed);
        let _ = fs::remove_file(&self.metadata_path);
    }
}

impl Drop for DesktopInstance {
    fn drop(&mut self) {
        self.prepare_shutdown();
        let _ = FileExt::unlock(&self._lock_file);
    }
}

pub fn show_main_window(app: &AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.unminimize();
        let _ = window.show();
        let _ = window.set_focus();
    }
}

fn authenticate_wake(mut stream: TcpStream, expected_secret: &str) -> bool {
    let _ = stream.set_read_timeout(Some(Duration::from_secs(2)));
    let _ = stream.set_write_timeout(Some(Duration::from_secs(2)));
    let cloned = match stream.try_clone() {
        Ok(value) => value,
        Err(_) => return false,
    };
    let mut reader = BufReader::new(cloned);
    let mut line = String::new();
    if reader.read_line(&mut line).is_err() || line.len() > 4096 {
        return false;
    }
    let value: serde_json::Value = match serde_json::from_str(line.trim()) {
        Ok(value) => value,
        Err(_) => return false,
    };
    let valid = value.get("schema").and_then(|item| item.as_str()) == Some("cwapi.desktop.wake.v1")
        && value.get("secret").and_then(|item| item.as_str()) == Some(expected_secret);
    let response = if valid {
        json!({"accepted": true})
    } else {
        json!({"accepted": false})
    };
    let _ = writeln!(stream, "{response}");
    valid
}

fn wake_existing(metadata_path: &Path) -> Result<(), String> {
    let mut last_error = "desktop instance metadata is not ready".to_string();
    for _ in 0..20 {
        match fs::read_to_string(metadata_path) {
            Ok(raw) => match serde_json::from_str::<InstanceMetadata>(&raw) {
                Ok(metadata) if metadata.schema == "cwapi.desktop.instance.v1" => {
                    let address: SocketAddr =
                        format!("127.0.0.1:{}", metadata.wake_port)
                            .parse()
                            .map_err(|error| format!("parse desktop wake address: {error}"))?;
                    match TcpStream::connect_timeout(&address, Duration::from_millis(500)) {
                        Ok(mut stream) => {
                            let request = json!({
                                "schema": "cwapi.desktop.wake.v1",
                                "secret": metadata.wake_secret,
                            });
                            writeln!(stream, "{request}")
                                .map_err(|error| format!("send desktop wake request: {error}"))?;
                            return Ok(());
                        }
                        Err(error) => {
                            last_error = format!("connect desktop wake listener: {error}")
                        }
                    }
                }
                Ok(_) => last_error = "desktop instance metadata schema mismatch".into(),
                Err(error) => last_error = format!("parse desktop instance metadata: {error}"),
            },
            Err(error) => last_error = format!("read desktop instance metadata: {error}"),
        }
        thread::sleep(Duration::from_millis(50));
    }
    Err(last_error)
}

fn write_metadata(path: &Path, metadata: &InstanceMetadata) -> Result<(), String> {
    let parent = path
        .parent()
        .ok_or_else(|| "desktop instance metadata has no parent".to_string())?;
    fs::create_dir_all(parent)
        .map_err(|error| format!("create desktop instance metadata directory: {error}"))?;
    let temporary = path.with_extension("json.tmp");
    let raw = serde_json::to_vec_pretty(metadata)
        .map_err(|error| format!("serialize desktop instance metadata: {error}"))?;
    fs::write(&temporary, raw)
        .map_err(|error| format!("write desktop instance metadata: {error}"))?;
    fs::rename(&temporary, path)
        .map_err(|error| format!("install desktop instance metadata: {error}"))?;
    Ok(())
}

fn random_secret() -> String {
    let mut bytes = [0u8; 32];
    OsRng.fill_bytes(&mut bytes);
    let mut output = String::with_capacity(64);
    for byte in bytes {
        use std::fmt::Write as _;
        let _ = write!(&mut output, "{byte:02x}");
    }
    output
}

#[cfg(test)]
mod tests {
    use super::*;

    fn temporary_root(label: &str) -> PathBuf {
        std::env::temp_dir().join(format!("cwapi-instance-{label}-{}", random_secret()))
    }

    #[test]
    fn second_instance_for_same_data_root_is_secondary() {
        let root = temporary_root("same-root");
        let first = match DesktopInstance::acquire(&root).expect("first acquire") {
            InstanceAcquire::Primary(instance) => instance,
            InstanceAcquire::SecondaryWoke => panic!("first instance became secondary"),
        };
        assert!(matches!(
            DesktopInstance::acquire(&root).expect("second acquire"),
            InstanceAcquire::SecondaryWoke
        ));
        drop(first);
        let _ = fs::remove_dir_all(root);
    }

    #[test]
    fn different_data_roots_can_each_be_primary() {
        let first_root = temporary_root("root-a");
        let second_root = temporary_root("root-b");
        let first = DesktopInstance::acquire(&first_root).expect("first root acquire");
        let second = DesktopInstance::acquire(&second_root).expect("second root acquire");
        assert!(matches!(first, InstanceAcquire::Primary(_)));
        assert!(matches!(second, InstanceAcquire::Primary(_)));
        drop(first);
        drop(second);
        let _ = fs::remove_dir_all(first_root);
        let _ = fs::remove_dir_all(second_root);
    }
}
