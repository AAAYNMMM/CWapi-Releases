use rand::{rngs::OsRng, RngCore};
use reqwest::blocking::Client;
use serde::Serialize;
use serde_json::Value;
use std::{
    env,
    fs::{self, OpenOptions},
    io::{BufRead, BufReader, Read, Write},
    path::{Path, PathBuf},
    process::{Child, Command, Stdio},
    sync::Mutex,
    thread,
    time::{Duration, SystemTime, UNIX_EPOCH},
};

#[cfg(windows)]
use std::os::windows::process::CommandExt;

const CREATE_NO_WINDOW: u32 = 0x0800_0000;

#[derive(Clone, Debug)]
struct BackendAuth {
    url: String,
    secret: String,
    pid: u32,
    started_at_epoch_ms: u128,
}

#[derive(Debug)]
struct BackendInner {
    child: Option<Child>,
    auth: Option<BackendAuth>,
    config_path: Option<PathBuf>,
    startup_error: Option<String>,
    setup_required: bool,
}

#[derive(Clone, Debug, Serialize)]
pub struct DesktopStatus {
    pub app_root: String,
    pub data_root: String,
    pub config_path: Option<String>,
    pub backend_running: bool,
    pub backend_pid: Option<u32>,
    pub backend_url: Option<String>,
    pub backend_started_at_epoch_ms: Option<u128>,
    pub setup_required: bool,
    pub startup_error: Option<String>,
}

pub struct BackendRuntime {
    app_root: PathBuf,
    data_root: PathBuf,
    dev_root: Option<PathBuf>,
    lifecycle: Mutex<()>,
    inner: Mutex<BackendInner>,
}

impl BackendRuntime {
    pub fn initialize() -> Self {
        let runtime = Self::initialize_deferred();
        let _ = runtime.start_if_configured();
        runtime
    }

    pub fn initialize_deferred() -> Self {
        let app_root = current_app_root().unwrap_or_else(|_| PathBuf::from("."));
        let data_root = app_root.join("CWapi-data");
        let mut startup_error = None;
        if let Err(error) = ensure_data_layout(&data_root) {
            startup_error = Some(error);
        }
        let dev_root = discover_dev_root();
        let config_path = discover_config_path(&data_root, dev_root.as_deref());
        let setup_required = config_path.is_none();
        Self {
            app_root,
            data_root,
            dev_root,
            lifecycle: Mutex::new(()),
            inner: Mutex::new(BackendInner {
                child: None,
                auth: None,
                config_path,
                startup_error,
                setup_required,
            }),
        }
    }

    pub fn start_if_configured(&self) -> Result<DesktopStatus, String> {
        let status = self.status();
        if status.setup_required || status.backend_running || status.startup_error.is_some() {
            return Ok(status);
        }
        if let Err(error) = self.start_backend() {
            let mut inner = self.inner.lock().map_err(|_| "backend state poisoned")?;
            inner.startup_error = Some(error.clone());
            return Err(error);
        }
        Ok(self.status())
    }

    pub fn status(&self) -> DesktopStatus {
        let mut inner = self.inner.lock().expect("backend state poisoned");
        if let Some(child) = inner.child.as_mut() {
            if let Ok(Some(exit)) = child.try_wait() {
                inner.startup_error = Some(format!("Python backend exited: {exit}"));
                inner.child = None;
                inner.auth = None;
            }
        }
        let auth = inner.auth.clone();
        DesktopStatus {
            app_root: self.app_root.display().to_string(),
            data_root: self.data_root.display().to_string(),
            config_path: inner
                .config_path
                .as_ref()
                .map(|path| path.display().to_string()),
            backend_running: inner.child.is_some() && auth.is_some(),
            backend_pid: auth.as_ref().map(|value| value.pid),
            backend_url: auth.as_ref().map(|value| value.url.clone()),
            backend_started_at_epoch_ms: auth.as_ref().map(|value| value.started_at_epoch_ms),
            setup_required: inner.setup_required,
            startup_error: inner.startup_error.clone(),
        }
    }

    pub fn data_root(&self) -> &Path {
        &self.data_root
    }

    pub fn request_get(&self, path: &str) -> Result<Value, String> {
        self.request("GET", path, None, Duration::from_secs(15))
    }

    pub fn request_post(&self, path: &str, body: Value) -> Result<Value, String> {
        self.request("POST", path, Some(body), Duration::from_secs(15))
    }

    pub fn request_maintenance(&self, body: Value) -> Result<Value, String> {
        self.request(
            "POST",
            "/v1/maintenance",
            Some(body),
            Duration::from_secs(620),
        )
    }

    pub fn restart(&self) -> Result<DesktopStatus, String> {
        if self.status().backend_running {
            if let Ok(current) = self.request_get("/v1/execution/current") {
                if !current.get("execution").map(Value::is_null).unwrap_or(true) {
                    return Err("cannot restart backend while a TASK is active".into());
                }
            }
        }
        self.shutdown();
        self.start_backend()?;
        Ok(self.status())
    }

    pub fn authorize_gmail(&self) -> Result<Value, String> {
        self.request(
            "POST",
            "/v1/auth/gmail",
            Some(serde_json::json!({})),
            Duration::from_secs(620),
        )
    }

    pub fn complete_setup(&self, credentials_source: &Path) -> Result<Value, String> {
        {
            let inner = self.inner.lock().map_err(|_| "backend state poisoned")?;
            if inner.child.is_some() || inner.auth.is_some() {
                return Err("backend is already running".into());
            }
        }
        let python = discover_python(&self.data_root, self.dev_root.as_deref())?;
        let mut command = Command::new(&python);
        command
            .arg("-m")
            .arg("cwapi.setup_bootstrap")
            .arg("--data-root")
            .arg(&self.data_root)
            .arg("--credentials-source")
            .arg(credentials_source)
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        if let Some(executable) = discover_setup_transport(&self.data_root) {
            command.arg("--transport-executable").arg(executable);
        }
        self.configure_python_command(&mut command);
        #[cfg(windows)]
        command.creation_flags(CREATE_NO_WINDOW);

        let output = command.output().map_err(|error| {
            format!(
                "start first-run bootstrap with {}: {error}",
                python.display()
            )
        })?;
        let stdout = String::from_utf8_lossy(&output.stdout);
        let stderr = String::from_utf8_lossy(&output.stderr);
        self.append_runtime_log("setup-bootstrap.stdout.log", stdout.as_bytes());
        self.append_runtime_log("setup-bootstrap.stderr.log", stderr.as_bytes());
        let payload = stdout
            .lines()
            .rev()
            .find_map(|line| serde_json::from_str::<Value>(line.trim()).ok())
            .ok_or_else(|| {
                format!(
                    "first-run bootstrap returned no JSON result (exit={}): {}",
                    output.status,
                    stderr.trim()
                )
            })?;
        if !output.status.success()
            || payload.get("schema").and_then(Value::as_str) != Some("cwapi.setup.completed.v1")
        {
            return Err(format!("first-run bootstrap failed: {payload}"));
        }

        let config_path = self.data_root.join("config").join("cwapi.yaml");
        if !config_path.is_file() {
            return Err("first-run bootstrap did not create cwapi.yaml".into());
        }
        {
            let mut inner = self.inner.lock().map_err(|_| "backend state poisoned")?;
            inner.config_path = Some(config_path);
            inner.setup_required = false;
            inner.startup_error = None;
        }
        if let Err(error) = self.start_backend() {
            let mut inner = self.inner.lock().map_err(|_| "backend state poisoned")?;
            inner.startup_error = Some(error.clone());
            return Err(error);
        }
        Ok(payload)
    }

    pub fn shutdown(&self) {
        let _lifecycle = self
            .lifecycle
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        let auth = {
            let inner = self.inner.lock().expect("backend state poisoned");
            inner.auth.clone()
        };
        let backend_pid = auth.as_ref().map(|value| value.pid);
        if auth.is_some() {
            let _ = self.request(
                "POST",
                "/v1/shutdown",
                Some(serde_json::json!({})),
                Duration::from_secs(5),
            );
        }

        let deadline = std::time::Instant::now() + Duration::from_secs(20);
        let mut graceful = false;
        loop {
            let finished = {
                let mut inner = self.inner.lock().expect("backend state poisoned");
                match inner.child.as_mut() {
                    None => true,
                    Some(child) => match child.try_wait() {
                        Ok(Some(_)) => {
                            inner.child = None;
                            inner.auth = None;
                            true
                        }
                        Ok(None) => false,
                        Err(_) => false,
                    },
                }
            };
            if finished {
                graceful = true;
                break;
            }
            if std::time::Instant::now() >= deadline {
                break;
            }
            thread::sleep(Duration::from_millis(100));
        }

        let mut forced = false;
        if !graceful {
            if let Some(pid) = backend_pid {
                forced = self.force_stop_owned_backend_tree(pid);
            }
            let mut inner = self.inner.lock().expect("backend state poisoned");
            if let Some(child) = inner.child.as_mut() {
                let _ = child.kill();
                let _ = child.wait();
                forced = true;
            }
            inner.child = None;
            inner.auth = None;
        }
        self.write_shutdown_receipt(backend_pid, graceful, forced);
    }

    fn request(
        &self,
        method: &str,
        path: &str,
        body: Option<Value>,
        timeout: Duration,
    ) -> Result<Value, String> {
        if !path.starts_with('/') {
            return Err("backend path must be absolute".into());
        }
        let auth = {
            let inner = self.inner.lock().map_err(|_| "backend state poisoned")?;
            inner.auth.clone().ok_or_else(|| {
                inner
                    .startup_error
                    .clone()
                    .unwrap_or_else(|| "backend is not running".into())
            })?
        };
        let client = Client::builder()
            .timeout(timeout)
            .build()
            .map_err(|error| format!("build backend HTTP client: {error}"))?;
        let url = format!("{}{}", auth.url, path);
        let request = match method {
            "GET" => client.get(url),
            "POST" => client.post(url),
            _ => return Err("unsupported backend method".into()),
        }
        .bearer_auth(auth.secret);
        let request = if let Some(value) = body {
            request.json(&value)
        } else {
            request
        };
        let response = request
            .send()
            .map_err(|error| format!("backend request failed: {error}"))?;
        let status = response.status();
        let text = response
            .text()
            .map_err(|error| format!("read backend response: {error}"))?;
        let value: Value = serde_json::from_str(&text)
            .map_err(|error| format!("backend returned invalid JSON: {error}"))?;
        if !status.is_success() {
            return Err(format!("backend HTTP {}: {}", status.as_u16(), value));
        }
        Ok(value)
    }

    fn start_backend(&self) -> Result<(), String> {
        let _lifecycle = self
            .lifecycle
            .lock()
            .map_err(|_| "backend lifecycle poisoned")?;
        let config_path = {
            let inner = self.inner.lock().map_err(|_| "backend state poisoned")?;
            inner
                .config_path
                .clone()
                .ok_or_else(|| "CWapi configuration is not available".to_string())?
        };
        let python = discover_python(&self.data_root, self.dev_root.as_deref())?;
        let mut command = Command::new(&python);
        command
            .arg("-m")
            .arg("cwapi.desktop_api")
            .arg("--config")
            .arg(config_path)
            .arg("--listen")
            .arg("127.0.0.1:0")
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());

        let secret = random_secret();
        command.env("CWAPI_DESKTOP_IPC_SECRET", &secret);
        self.configure_python_command(&mut command);
        #[cfg(windows)]
        command.creation_flags(CREATE_NO_WINDOW);

        let mut child = command
            .spawn()
            .map_err(|error| format!("start Python backend with {}: {error}", python.display()))?;
        let pid = child.id();
        let stdout = child
            .stdout
            .take()
            .ok_or_else(|| "Python backend stdout is unavailable".to_string())?;
        let stderr = child
            .stderr
            .take()
            .ok_or_else(|| "Python backend stderr is unavailable".to_string())?;

        let runtime_logs = self.data_root.join("logs").join("runtime");
        let stderr_path = runtime_logs.join("desktop-backend.stderr.log");
        spawn_stream_capture(stderr, stderr_path);

        let mut reader = BufReader::new(stdout);
        let mut line = String::new();
        let mut ready: Option<Value> = None;
        for _ in 0..20 {
            line.clear();
            let read = reader
                .read_line(&mut line)
                .map_err(|error| format!("read Python backend readiness: {error}"))?;
            if read == 0 {
                break;
            }
            if let Ok(value) = serde_json::from_str::<Value>(line.trim()) {
                if value.get("schema").and_then(Value::as_str) == Some("cwapi.desktop.ready.v1") {
                    ready = Some(value);
                    break;
                }
                if value.get("schema").and_then(Value::as_str) == Some("cwapi.desktop.error.v1") {
                    let _ = child.wait();
                    return Err(format!("Python backend startup failed: {value}"));
                }
            }
        }
        let ready = match ready {
            Some(value) => value,
            None => {
                let exit = child.try_wait().ok().flatten();
                let _ = child.kill();
                let _ = child.wait();
                return Err(format!(
                    "Python backend produced no readiness message; exit={exit:?}"
                ));
            }
        };
        let url = ready
            .get("url")
            .and_then(Value::as_str)
            .filter(|value| value.starts_with("http://127.0.0.1:"))
            .ok_or_else(|| "Python backend returned a non-loopback URL".to_string())?
            .to_string();

        let stdout_path = runtime_logs.join("desktop-backend.stdout.log");
        spawn_reader_capture(reader, stdout_path);
        let started_at_epoch_ms = now_epoch_ms();
        let mut inner = self.inner.lock().map_err(|_| "backend state poisoned")?;
        inner.child = Some(child);
        inner.auth = Some(BackendAuth {
            url,
            secret,
            pid,
            started_at_epoch_ms,
        });
        inner.startup_error = None;
        inner.setup_required = false;
        Ok(())
    }

    fn configure_python_command(&self, command: &mut Command) {
        if let Some(root) = self.dev_root.as_deref() {
            let source = root.join("src");
            let inherited = env::var_os("PYTHONPATH").unwrap_or_default();
            let mut paths = vec![source];
            paths.extend(env::split_paths(&inherited));
            if let Ok(joined) = env::join_paths(paths) {
                command.env("PYTHONPATH", joined);
            }
            command.current_dir(root);
        } else {
            let packaged_source = self.data_root.join("app").join("src");
            if packaged_source.is_dir() {
                command.env("PYTHONPATH", packaged_source);
            }
            let git_root = self.data_root.join("runtime").join("git");
            let git_executable = git_root.join("cmd").join("git.exe");
            let mut runtime_paths = vec![
                git_root.join("cmd"),
                git_root.join("mingw64").join("bin"),
                git_root.join("usr").join("bin"),
                self.data_root.join("runtime").join("python"),
                self.data_root.join("runtime").join("node"),
                self.data_root.join("runtime").join("transport"),
            ];
            runtime_paths.retain(|path| path.is_dir());
            runtime_paths.extend(env::split_paths(&env::var_os("PATH").unwrap_or_default()));
            if let Ok(joined) = env::join_paths(runtime_paths) {
                command.env("PATH", joined);
            }
            if git_executable.is_file() {
                command.env("CWAPI_GIT_EXECUTABLE", git_executable);
            }
            command.current_dir(&self.app_root);
        }
    }

    fn append_runtime_log(&self, file_name: &str, content: &[u8]) {
        let path = self.data_root.join("logs").join("runtime").join(file_name);
        if let Some(parent) = path.parent() {
            let _ = fs::create_dir_all(parent);
        }
        if let Ok(mut file) = OpenOptions::new().create(true).append(true).open(path) {
            let _ = file.write_all(content);
            let _ = file.flush();
        }
    }

    #[cfg(windows)]
    fn force_stop_owned_backend_tree(&self, pid: u32) -> bool {
        let status = Command::new("taskkill")
            .args(["/PID", &pid.to_string(), "/T", "/F"])
            .stdin(Stdio::null())
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .creation_flags(CREATE_NO_WINDOW)
            .status();
        status.map(|value| value.success()).unwrap_or(false)
    }

    #[cfg(not(windows))]
    fn force_stop_owned_backend_tree(&self, _pid: u32) -> bool {
        false
    }

    fn write_shutdown_receipt(&self, backend_pid: Option<u32>, graceful: bool, forced: bool) {
        let path = self.data_root.join("state").join("last-shutdown.json");
        let payload = serde_json::json!({
            "schema": "cwapi.desktop.shutdown.v1",
            "backend_pid": backend_pid,
            "graceful": graceful,
            "forced": forced,
            "finished_at_epoch_ms": now_epoch_ms(),
        });
        if let Ok(raw) = serde_json::to_vec_pretty(&payload) {
            let temporary = path.with_extension("json.tmp");
            if fs::write(&temporary, raw).is_ok() {
                let _ = fs::rename(temporary, path);
            }
        }
    }
}

impl Drop for BackendRuntime {
    fn drop(&mut self) {
        self.shutdown();
    }
}

fn current_app_root() -> Result<PathBuf, String> {
    let executable = env::current_exe().map_err(|error| format!("locate CWapi.exe: {error}"))?;
    executable
        .parent()
        .map(Path::to_path_buf)
        .ok_or_else(|| "CWapi.exe has no parent directory".to_string())
}

fn ensure_data_layout(data_root: &Path) -> Result<(), String> {
    for relative in [
        "app",
        "runtime",
        "config",
        "secrets",
        "state",
        "state/codex-home",
        "state/runtime-home/appdata",
        "state/runtime-home/localappdata",
        "repos",
        "worktrees",
        "logs/runtime",
        "logs/tasks",
        "logs/browser",
        "results",
        "cache",
        "temp",
    ] {
        fs::create_dir_all(data_root.join(relative))
            .map_err(|error| format!("create CWapi-data/{relative}: {error}"))?;
    }
    Ok(())
}

fn discover_dev_root() -> Option<PathBuf> {
    if let Some(value) = env::var_os("CWAPI_DESKTOP_DEV_ROOT") {
        let root = PathBuf::from(value);
        if root.join("pyproject.toml").is_file() && root.join("src/cwapi").is_dir() {
            return Some(root);
        }
    }
    #[cfg(debug_assertions)]
    {
        let manifest = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
        for ancestor in manifest.ancestors() {
            if ancestor.join("pyproject.toml").is_file() && ancestor.join("src/cwapi").is_dir() {
                return Some(ancestor.to_path_buf());
            }
        }
    }
    None
}

fn discover_config_path(data_root: &Path, dev_root: Option<&Path>) -> Option<PathBuf> {
    let portable = data_root.join("config").join("cwapi.yaml");
    if portable.is_file() {
        return Some(portable);
    }
    if let Some(value) = env::var_os("CWAPI_DESKTOP_DEV_CONFIG") {
        let candidate = PathBuf::from(value);
        if candidate.is_file() {
            return Some(candidate);
        }
    }
    dev_root
        .map(|root| root.join("config").join("cwapi.yaml"))
        .filter(|path| path.is_file())
}

fn discover_python(data_root: &Path, dev_root: Option<&Path>) -> Result<PathBuf, String> {
    let packaged = data_root.join("runtime").join("python").join("python.exe");
    if packaged.is_file() {
        return Ok(packaged);
    }
    if dev_root.is_some() {
        if let Some(value) = env::var_os("CWAPI_DESKTOP_DEV_PYTHON") {
            return Ok(PathBuf::from(value));
        }
        return Ok(PathBuf::from("python"));
    }
    Err(format!(
        "portable Python runtime is missing: {}",
        packaged.display()
    ))
}

fn discover_setup_transport(data_root: &Path) -> Option<PathBuf> {
    let packaged = data_root
        .join("runtime")
        .join("transport")
        .join(if cfg!(windows) {
            "cwapi-transport.exe"
        } else {
            "cwapi-transport"
        });
    if packaged.is_file() {
        return Some(packaged);
    }
    env::var_os("CWAPI_DESKTOP_DEV_TRANSPORT")
        .map(PathBuf::from)
        .filter(|path| path.is_file())
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

fn now_epoch_ms() -> u128 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis()
}

fn spawn_stream_capture<R>(mut stream: R, path: PathBuf)
where
    R: Read + Send + 'static,
{
    thread::spawn(move || {
        if let Some(parent) = path.parent() {
            let _ = fs::create_dir_all(parent);
        }
        if let Ok(mut file) = OpenOptions::new().create(true).append(true).open(path) {
            let _ = std::io::copy(&mut stream, &mut file);
            let _ = file.flush();
        }
    });
}

fn spawn_reader_capture<R>(mut reader: BufReader<R>, path: PathBuf)
where
    R: Read + Send + 'static,
{
    thread::spawn(move || {
        if let Some(parent) = path.parent() {
            let _ = fs::create_dir_all(parent);
        }
        if let Ok(mut file) = OpenOptions::new().create(true).append(true).open(path) {
            let _ = std::io::copy(&mut reader, &mut file);
            let _ = file.flush();
        }
    });
}
