mod backend;
mod instance;
mod process_registry;

use backend::{BackendRuntime, DesktopStatus};
use instance::{show_main_window, DesktopInstance, InstanceAcquire};
use process_registry::ProcessOwnershipRegistry;
use serde_json::{json, Value};
#[cfg(debug_assertions)]
use std::time::Duration;
use std::{
    path::PathBuf,
    sync::atomic::{AtomicBool, Ordering},
    thread,
};
use tauri::{
    menu::{Menu, MenuItem},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    AppHandle, Manager, State, WindowEvent,
};
use tauri_plugin_dialog::DialogExt;

struct ShutdownState(AtomicBool);

#[tauri::command]
fn desktop_status(runtime: State<'_, BackendRuntime>) -> DesktopStatus {
    runtime.status()
}

#[tauri::command(async)]
fn desktop_frontend_ready(runtime: State<'_, BackendRuntime>) -> Result<Value, String> {
    let marker = runtime
        .data_root()
        .join("state")
        .join("frontend-ready.json");
    let status = runtime.status();
    let payload = json!({
        "schema": "cwapi.desktop.frontend-ready.v1",
        "desktop_pid": std::process::id(),
        "backend_running": status.backend_running,
        "startup_error": status.startup_error,
    });
    let raw = serde_json::to_vec_pretty(&payload)
        .map_err(|error| format!("serialize frontend readiness marker: {error}"))?;
    let temporary = marker.with_extension("json.tmp");
    std::fs::write(&temporary, raw)
        .map_err(|error| format!("write frontend readiness marker: {error}"))?;
    std::fs::rename(&temporary, &marker)
        .map_err(|error| format!("publish frontend readiness marker: {error}"))?;
    Ok(payload)
}

#[tauri::command(async)]
fn backend_health(runtime: State<'_, BackendRuntime>) -> Result<Value, String> {
    runtime.request_get("/health")
}

#[tauri::command(async)]
fn backend_tasks(runtime: State<'_, BackendRuntime>, limit: Option<u32>) -> Result<Value, String> {
    let bounded = limit.unwrap_or(50).clamp(1, 200);
    runtime.request_get(&format!("/v1/tasks?limit={bounded}"))
}

#[tauri::command(async)]
fn backend_task(runtime: State<'_, BackendRuntime>, task_id: String) -> Result<Value, String> {
    let encoded = urlencoding::encode(&task_id);
    runtime.request_get(&format!("/v1/tasks/{encoded}"))
}

#[tauri::command(async)]
fn backend_submit_task(
    runtime: State<'_, BackendRuntime>,
    payload: Value,
) -> Result<Value, String> {
    runtime.request_post("/v1/tasks", payload)
}

#[tauri::command(async)]
fn backend_current_execution(runtime: State<'_, BackendRuntime>) -> Result<Value, String> {
    runtime.request_get("/v1/execution/current")
}

#[tauri::command(async)]
fn backend_runtime_state(runtime: State<'_, BackendRuntime>) -> Result<Value, String> {
    runtime.request_get("/v1/runtime/state")
}

#[tauri::command(async)]
fn backend_workbench(runtime: State<'_, BackendRuntime>) -> Result<Value, String> {
    runtime.request_get("/v1/workbench")
}

#[tauri::command(async)]
fn backend_management(runtime: State<'_, BackendRuntime>) -> Result<Value, String> {
    runtime.request_get("/v1/management")
}

#[tauri::command(async)]
fn backend_doctor(runtime: State<'_, BackendRuntime>) -> Result<Value, String> {
    runtime.request_get("/v1/doctor")
}

#[tauri::command(async)]
fn backend_save_settings(
    runtime: State<'_, BackendRuntime>,
    payload: Value,
) -> Result<Value, String> {
    runtime.request_post("/v1/config/save", payload)
}

#[tauri::command(async)]
fn backend_validate_settings(
    runtime: State<'_, BackendRuntime>,
    payload: Value,
) -> Result<Value, String> {
    runtime.request_post("/v1/config/validate", payload)
}

#[tauri::command(async)]
fn backend_maintenance(
    runtime: State<'_, BackendRuntime>,
    action: String,
    task_id: Option<String>,
) -> Result<Value, String> {
    runtime.request_maintenance(json!({"action": action, "task_id": task_id}))
}

#[tauri::command(async)]
fn backend_gmail_status(runtime: State<'_, BackendRuntime>) -> Result<Value, String> {
    runtime.request_get("/v1/auth/gmail/status")
}

#[tauri::command(async)]
fn backend_execution_events(
    runtime: State<'_, BackendRuntime>,
    task_id: Option<String>,
    limit: Option<u32>,
    tail_bytes: Option<u32>,
) -> Result<Value, String> {
    let bounded_limit = limit.unwrap_or(300).clamp(20, 500);
    let bounded_tail = tail_bytes.unwrap_or(32 * 1024).clamp(1024, 64 * 1024);
    let mut path = format!("/v1/execution/events?limit={bounded_limit}&tail_bytes={bounded_tail}");
    if let Some(task_id) = task_id.filter(|value| !value.trim().is_empty()) {
        path.push_str("&task_id=");
        path.push_str(&urlencoding::encode(&task_id));
    }
    runtime.request_get(&path)
}

#[tauri::command(async)]
fn backend_config(runtime: State<'_, BackendRuntime>) -> Result<Value, String> {
    runtime.request_get("/v1/config")
}

#[tauri::command(async)]
fn backend_validate_config(runtime: State<'_, BackendRuntime>) -> Result<Value, String> {
    runtime.request_post("/v1/config/validate", json!({}))
}

#[tauri::command(async)]
fn backend_processes(runtime: State<'_, BackendRuntime>) -> Result<Value, String> {
    runtime.request_get("/v1/processes")
}

#[tauri::command(async)]
fn desktop_processes(
    runtime: State<'_, BackendRuntime>,
    registry: State<'_, ProcessOwnershipRegistry>,
) -> Result<Value, String> {
    let backend = if runtime.status().backend_running {
        runtime.request_get("/v1/processes")?
    } else {
        json!({"processes": []})
    };
    Ok(json!({
        "desktop": registry.snapshot(),
        "backend": backend.get("processes").cloned().unwrap_or_else(|| json!([])),
    }))
}

#[tauri::command(async)]
fn backend_cancel_task(
    runtime: State<'_, BackendRuntime>,
    task_id: String,
    reason: Option<String>,
) -> Result<Value, String> {
    runtime.request_post(
        "/v1/operations/cancel",
        json!({
            "task_id": task_id,
            "reason": reason.unwrap_or_else(|| "Cancelled from CWapi desktop.".into()),
        }),
    )
}

#[tauri::command(async)]
fn backend_authorize_gmail(runtime: State<'_, BackendRuntime>) -> Result<Value, String> {
    runtime.authorize_gmail()
}

#[tauri::command(async)]
fn desktop_restart_backend(
    runtime: State<'_, BackendRuntime>,
    registry: State<'_, ProcessOwnershipRegistry>,
) -> Result<DesktopStatus, String> {
    let status = runtime.restart()?;
    if let Some(pid) = status.backend_pid {
        registry.register(
            pid,
            "python-backend",
            "desktop-shell",
            Some(std::process::id()),
            None,
        );
    }
    Ok(status)
}

#[tauri::command(async)]
fn desktop_remove_gmail_authorization(
    runtime: State<'_, BackendRuntime>,
    registry: State<'_, ProcessOwnershipRegistry>,
) -> Result<Value, String> {
    let result = runtime.request_post("/v1/auth/gmail/remove", json!({}))?;
    let status = runtime.restart()?;
    if let Some(pid) = status.backend_pid {
        registry.register(
            pid,
            "python-backend",
            "desktop-shell",
            Some(std::process::id()),
            None,
        );
    }
    Ok(json!({"result": result, "status": status}))
}

#[tauri::command]
async fn setup_pick_credentials(
    app: AppHandle,
    runtime: State<'_, BackendRuntime>,
    registry: State<'_, ProcessOwnershipRegistry>,
) -> Result<Value, String> {
    if !runtime.status().setup_required {
        return Ok(json!({
            "cancelled": false,
            "already_configured": true,
            "status": runtime.status(),
        }));
    }
    let selected = app
        .dialog()
        .file()
        .add_filter("Google OAuth credentials", &["json"])
        .blocking_pick_file();
    let Some(selected) = selected else {
        return Ok(json!({"cancelled": true}));
    };
    let path = selected
        .into_path()
        .map_err(|error| format!("无法读取所选 credentials.json 路径：{error}"))?;
    let setup = runtime.complete_setup(&path)?;
    let status = runtime.status();
    if let Some(pid) = status.backend_pid {
        registry.register(
            pid,
            "python-backend",
            "desktop-shell",
            Some(std::process::id()),
            None,
        );
    }
    Ok(json!({
        "cancelled": false,
        "already_configured": false,
        "setup": setup,
        "status": status,
    }))
}

#[tauri::command]
async fn desktop_replace_gmail_credentials(
    app: AppHandle,
    runtime: State<'_, BackendRuntime>,
    registry: State<'_, ProcessOwnershipRegistry>,
) -> Result<Value, String> {
    let selected = app
        .dialog()
        .file()
        .add_filter("Google OAuth credentials", &["json"])
        .blocking_pick_file();
    let Some(selected) = selected else {
        return Ok(json!({"cancelled": true}));
    };
    let source = selected
        .into_path()
        .map_err(|error| format!("无法读取所选 credentials.json 路径：{error}"))?;
    let raw =
        std::fs::read(&source).map_err(|error| format!("读取 OAuth 配置文件失败：{error}"))?;
    let parsed: Value = serde_json::from_slice(&raw)
        .map_err(|error| format!("OAuth 配置文件不是有效 JSON：{error}"))?;
    let oauth = parsed
        .get("installed")
        .or_else(|| parsed.get("web"))
        .and_then(Value::as_object)
        .ok_or_else(|| "OAuth 配置文件缺少 installed/web 客户端配置".to_string())?;
    for key in ["client_id", "client_secret", "auth_uri", "token_uri"] {
        let valid = oauth
            .get(key)
            .and_then(Value::as_str)
            .map(|value| !value.trim().is_empty())
            .unwrap_or(false);
        if !valid {
            return Err(format!("OAuth 配置文件缺少 {key}"));
        }
    }
    let management = runtime.request_get("/v1/management")?;
    let destination_raw = management
        .pointer("/config/read_only/credentials_path")
        .and_then(Value::as_str)
        .ok_or_else(|| "CWapi 未提供受管 OAuth 配置路径".to_string())?;
    let destination = PathBuf::from(destination_raw);
    let parent = destination
        .parent()
        .ok_or_else(|| "OAuth 配置路径没有父目录".to_string())?;
    std::fs::create_dir_all(parent).map_err(|error| format!("创建 OAuth 配置目录失败：{error}"))?;
    let data_root = runtime
        .data_root()
        .canonicalize()
        .map_err(|error| format!("resolve CWapi data root: {error}"))?;
    let parent_root = parent
        .canonicalize()
        .map_err(|error| format!("resolve OAuth config directory: {error}"))?;
    if !parent_root.starts_with(&data_root) {
        return Err("OAuth 配置目标路径不在 CWapi 数据目录内".into());
    }
    let temporary = destination.with_extension("json.tmp");
    std::fs::write(&temporary, &raw)
        .map_err(|error| format!("写入临时 OAuth 配置失败：{error}"))?;
    if destination.exists() {
        std::fs::remove_file(&destination)
            .map_err(|error| format!("替换旧 OAuth 配置失败：{error}"))?;
    }
    std::fs::rename(&temporary, &destination)
        .map_err(|error| format!("应用新的 OAuth 配置失败：{error}"))?;
    let removed = runtime.request_post("/v1/auth/gmail/remove", json!({}))?;
    let status = runtime.restart()?;
    if let Some(pid) = status.backend_pid {
        registry.register(
            pid,
            "python-backend",
            "desktop-shell",
            Some(std::process::id()),
            None,
        );
    }
    let authorization = runtime.authorize_gmail()?;
    Ok(
        json!({"cancelled": false, "credentials_path": destination.display().to_string(), "removed": removed, "authorization": authorization, "status": runtime.status()}),
    )
}

#[tauri::command]
async fn desktop_pick_directory(app: AppHandle) -> Result<Value, String> {
    let selected = app.dialog().file().blocking_pick_folder();
    let Some(selected) = selected else {
        return Ok(json!({"cancelled": true, "path": null}));
    };
    let path = selected
        .into_path()
        .map_err(|error| format!("无法读取所选文件夹路径：{error}"))?;
    Ok(json!({"cancelled": false, "path": path.display().to_string()}))
}

#[tauri::command(async)]
fn desktop_reveal_path(runtime: State<'_, BackendRuntime>, path: String) -> Result<Value, String> {
    let candidate = PathBuf::from(path);
    if !candidate.exists() {
        return Err("requested path does not exist".into());
    }
    let canonical = candidate
        .canonicalize()
        .map_err(|error| format!("resolve requested path: {error}"))?;
    let data_root = runtime
        .data_root()
        .canonicalize()
        .map_err(|error| format!("resolve CWapi data root: {error}"))?;
    let mut allowed_roots = vec![data_root];
    if let Ok(value) = runtime.request_get("/v1/reveal-roots") {
        if let Some(items) = value.get("roots").and_then(Value::as_array) {
            for item in items {
                let Some(raw) = item.as_str() else {
                    continue;
                };
                if let Ok(root) = PathBuf::from(raw).canonicalize() {
                    allowed_roots.push(root);
                }
            }
        }
    }
    if !allowed_roots.iter().any(|root| canonical.starts_with(root)) {
        return Err("requested path is outside CWapi managed or user-configured roots".into());
    }
    #[cfg(target_os = "windows")]
    {
        let mut command = std::process::Command::new("explorer.exe");
        if canonical.is_file() {
            command.arg(format!("/select,{}", canonical.display()));
        } else {
            command.arg(&canonical);
        }
        command
            .spawn()
            .map_err(|error| format!("open Windows Explorer: {error}"))?;
    }
    #[cfg(not(target_os = "windows"))]
    {
        return Err("reveal path is supported by the Windows desktop build only".into());
    }
    Ok(json!({"opened": true}))
}

#[tauri::command]
fn desktop_request_exit(app: AppHandle) -> Result<Value, String> {
    begin_application_shutdown(app);
    Ok(json!({"accepted": true}))
}

fn portable_data_root() -> Result<PathBuf, String> {
    let executable =
        std::env::current_exe().map_err(|error| format!("locate CWapi.exe: {error}"))?;
    let app_root = executable
        .parent()
        .ok_or_else(|| "CWapi.exe has no parent directory".to_string())?;
    Ok(app_root.join("CWapi-data"))
}

fn begin_application_shutdown(app: AppHandle) {
    let Some(shutdown) = app.try_state::<ShutdownState>() else {
        app.exit(0);
        return;
    };
    if shutdown
        .0
        .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
        .is_err()
    {
        return;
    }
    thread::spawn(move || {
        let backend_pid = app
            .try_state::<BackendRuntime>()
            .and_then(|runtime| runtime.status().backend_pid);
        if let Some(runtime) = app.try_state::<BackendRuntime>() {
            runtime.shutdown();
        }
        if let Some(registry) = app.try_state::<ProcessOwnershipRegistry>() {
            if let Some(pid) = backend_pid {
                registry.mark_stopped(pid);
            }
            registry.mark_stopped(std::process::id());
        }
        if let Some(instance) = app.try_state::<DesktopInstance>() {
            instance.prepare_shutdown();
        }
        app.exit(0);
    });
}

fn start_backend_in_background(app: AppHandle) -> std::io::Result<()> {
    thread::Builder::new()
        .name("cwapi-backend-startup".into())
        .spawn(move || {
            let Some(runtime) = app.try_state::<BackendRuntime>() else {
                return;
            };
            let Ok(status) = runtime.start_if_configured() else {
                return;
            };
            let Some(pid) = status.backend_pid else {
                return;
            };
            if let Some(registry) = app.try_state::<ProcessOwnershipRegistry>() {
                registry.register(
                    pid,
                    "python-backend",
                    "desktop-shell",
                    Some(std::process::id()),
                    None,
                );
            }
        })
        .map(|_| ())
}

#[cfg(debug_assertions)]
fn setup_acceptance_auto_exit(app: &tauri::App) {
    let Ok(raw) = std::env::var("CWAPI_DESKTOP_ACCEPTANCE_EXIT_AFTER_MS") else {
        return;
    };
    let Ok(milliseconds) = raw.parse::<u64>() else {
        return;
    };
    if milliseconds == 0 || milliseconds > 120_000 {
        return;
    }
    let app_handle = app.handle().clone();
    thread::Builder::new()
        .name("cwapi-desktop-acceptance-exit".into())
        .spawn(move || {
            thread::sleep(Duration::from_millis(milliseconds));
            begin_application_shutdown(app_handle);
        })
        .ok();
}

#[cfg(not(debug_assertions))]
fn setup_acceptance_auto_exit(_app: &tauri::App) {}

fn setup_tray(app: &tauri::App) -> tauri::Result<()> {
    let open = MenuItem::with_id(app, "open-cwapi", "打开 CWapi", true, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit-cwapi", "退出 CWapi", true, None::<&str>)?;
    let menu = Menu::with_items(app, &[&open, &quit])?;

    let mut builder = TrayIconBuilder::with_id("cwapi-tray")
        .menu(&menu)
        .show_menu_on_left_click(false)
        .tooltip("CWapi")
        .on_menu_event(|app, event| match event.id.as_ref() {
            "open-cwapi" => show_main_window(app),
            "quit-cwapi" => begin_application_shutdown(app.clone()),
            _ => {}
        })
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click {
                button: MouseButton::Left,
                button_state: MouseButtonState::Up,
                ..
            } = event
            {
                show_main_window(tray.app_handle());
            }
        });
    if let Some(icon) = app.default_window_icon() {
        builder = builder.icon(icon.clone());
    }
    builder.build(app)?;
    Ok(())
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .setup(|app| {
            let data_root = portable_data_root().map_err(std::io::Error::other)?;
            match DesktopInstance::acquire(&data_root).map_err(std::io::Error::other)? {
                InstanceAcquire::SecondaryWoke => std::process::exit(0),
                InstanceAcquire::Primary(instance) => {
                    instance
                        .start_listener(app.handle().clone())
                        .map_err(std::io::Error::other)?;
                    app.manage(instance);
                }
            }

            let _ = std::fs::remove_file(data_root.join("state").join("frontend-ready.json"));
            let registry = ProcessOwnershipRegistry::new(
                data_root.join("state").join("process-ownership.json"),
            );
            registry.register(
                std::process::id(),
                "desktop-shell",
                "windows-user",
                None,
                None,
            );

            let runtime = BackendRuntime::initialize_deferred();
            app.manage(registry);
            app.manage(runtime);
            app.manage(ShutdownState(AtomicBool::new(false)));
            setup_tray(app)?;
            setup_acceptance_auto_exit(app);
            start_backend_in_background(app.handle().clone())?;
            Ok(())
        })
        .on_window_event(|window, event| {
            if window.label() != "main" {
                return;
            }
            if let WindowEvent::CloseRequested { api, .. } = event {
                api.prevent_close();
                let _ = window.hide();
            }
        })
        .invoke_handler(tauri::generate_handler![
            desktop_status,
            desktop_frontend_ready,
            backend_health,
            backend_tasks,
            backend_task,
            backend_submit_task,
            backend_current_execution,
            backend_runtime_state,
            backend_workbench,
            backend_management,
            backend_doctor,
            backend_save_settings,
            backend_validate_settings,
            backend_maintenance,
            backend_gmail_status,
            backend_execution_events,
            backend_config,
            backend_validate_config,
            backend_processes,
            desktop_processes,
            backend_cancel_task,
            backend_authorize_gmail,
            desktop_restart_backend,
            desktop_remove_gmail_authorization,
            setup_pick_credentials,
            desktop_replace_gmail_credentials,
            desktop_pick_directory,
            desktop_reveal_path,
            desktop_request_exit,
        ])
        .run(tauri::generate_context!())
        .expect("failed to run CWapi desktop shell");
}
