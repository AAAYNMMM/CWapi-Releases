use serde::Serialize;
use std::{
    collections::BTreeMap,
    fs,
    path::PathBuf,
    sync::Mutex,
    time::{SystemTime, UNIX_EPOCH},
};

#[derive(Clone, Debug, Serialize)]
pub struct OwnedProcess {
    pub pid: u32,
    pub component: String,
    pub owner: String,
    pub parent_pid: Option<u32>,
    pub task_id: Option<String>,
    pub started_at_epoch_ms: u128,
    pub state: String,
}

pub struct ProcessOwnershipRegistry {
    path: PathBuf,
    entries: Mutex<BTreeMap<u32, OwnedProcess>>,
}

impl ProcessOwnershipRegistry {
    pub fn new(path: PathBuf) -> Self {
        Self {
            path,
            entries: Mutex::new(BTreeMap::new()),
        }
    }

    pub fn register(
        &self,
        pid: u32,
        component: impl Into<String>,
        owner: impl Into<String>,
        parent_pid: Option<u32>,
        task_id: Option<String>,
    ) {
        let mut entries = self.entries.lock().expect("process registry poisoned");
        entries.insert(
            pid,
            OwnedProcess {
                pid,
                component: component.into(),
                owner: owner.into(),
                parent_pid,
                task_id,
                started_at_epoch_ms: now_epoch_ms(),
                state: "running".into(),
            },
        );
        drop(entries);
        self.persist();
    }

    pub fn mark_stopped(&self, pid: u32) {
        let mut entries = self.entries.lock().expect("process registry poisoned");
        if let Some(entry) = entries.get_mut(&pid) {
            entry.state = "stopped".into();
        }
        drop(entries);
        self.persist();
    }

    pub fn snapshot(&self) -> Vec<OwnedProcess> {
        self.entries
            .lock()
            .expect("process registry poisoned")
            .values()
            .cloned()
            .collect()
    }

    pub fn clear_stopped(&self) {
        let mut entries = self.entries.lock().expect("process registry poisoned");
        entries.retain(|_, entry| entry.state != "stopped");
        drop(entries);
        self.persist();
    }

    fn persist(&self) {
        let snapshot = self.snapshot();
        if let Some(parent) = self.path.parent() {
            let _ = fs::create_dir_all(parent);
        }
        let temporary = self.path.with_extension("json.tmp");
        if let Ok(raw) = serde_json::to_vec_pretty(&serde_json::json!({
            "schema": "cwapi.process-ownership.v1",
            "processes": snapshot,
        })) {
            if fs::write(&temporary, raw).is_ok() {
                let _ = fs::rename(&temporary, &self.path);
            }
        }
    }
}

fn now_epoch_ms() -> u128 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis()
}
