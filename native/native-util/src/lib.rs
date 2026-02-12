use jwalk::WalkDir;
use napi_derive::napi;
use rayon::prelude::*;
use std::collections::HashSet;
use std::sync::{
    atomic::{AtomicBool, Ordering},
    Arc, Mutex,
};
use std::thread;
use std::time::Duration;
use sysinfo::Disks;
use windows::core::PWSTR;
use windows::Win32::Foundation::{BOOL, HWND, LPARAM};
use windows::Win32::System::Threading::{
    OpenProcess, QueryFullProcessImageNameW, PROCESS_NAME_WIN32, PROCESS_QUERY_LIMITED_INFORMATION,
};
use windows::Win32::UI::WindowsAndMessaging::GetForegroundWindow;
use windows::Win32::UI::WindowsAndMessaging::{
    EnumWindows, GetWindowThreadProcessId, IsIconic, IsWindowVisible,
};

#[napi]
pub enum ProcessWindowState {
    Open,
    Minimized,
    NotFound,
}

struct EnumState {
    target_pid: u32,
    result: ProcessWindowState,
}

struct ZOrderState {
    target_pids: HashSet<u32>,
    found_pid: Option<u32>,
}

#[napi]
pub fn get_process_window_state(pid: u32) -> ProcessWindowState {
    let mut state = EnumState {
        target_pid: pid,
        result: ProcessWindowState::NotFound,
    };

    unsafe {
        let _ = EnumWindows(
            Some(enum_window_callback),
            LPARAM(&mut state as *mut EnumState as isize),
        );
    }

    state.result
}

#[napi]
pub fn get_topmost_pid(pids: Vec<u32>) -> Option<u32> {
    if pids.is_empty() {
        return None;
    }

    let mut state = ZOrderState {
        target_pids: pids.into_iter().collect(),
        found_pid: None,
    };

    unsafe {
        let _ = EnumWindows(
            Some(enum_z_order_callback),
            LPARAM(&mut state as *mut ZOrderState as isize),
        );
    }

    state.found_pid
}

unsafe extern "system" fn enum_window_callback(hwnd: HWND, lparam: LPARAM) -> BOOL {
    let state = &mut *(lparam.0 as *mut EnumState);
    let mut window_pid = 0;

    GetWindowThreadProcessId(hwnd, Some(&mut window_pid));

    if window_pid == state.target_pid {
        if IsWindowVisible(hwnd).as_bool() {
            if IsIconic(hwnd).as_bool() {
                state.result = ProcessWindowState::Minimized;
            } else {
                state.result = ProcessWindowState::Open;
            }
            return BOOL(0);
        }
    }

    BOOL(1)
}

unsafe extern "system" fn enum_z_order_callback(hwnd: HWND, lparam: LPARAM) -> BOOL {
    let state = &mut *(lparam.0 as *mut ZOrderState);

    let mut window_pid = 0;
    GetWindowThreadProcessId(hwnd, Some(&mut window_pid));

    if state.target_pids.contains(&window_pid) {
        if IsWindowVisible(hwnd).as_bool() {
            state.found_pid = Some(window_pid);
            return BOOL(0);
        }
    }
    BOOL(1)
}

struct FocusTracker {
    pub history: Vec<u32>, // Store PID history
}

impl FocusTracker {
    fn new() -> Self {
        Self {
            history: Vec::new(),
        }
    }

    fn push(&mut self, pid: u32) {
        if self.history.last() != Some(&pid) {
            self.history.push(pid);
            if self.history.len() > 30 {
                self.history.remove(0);
            }
        }
    }
}

lazy_static::lazy_static! {
    static ref TRACKER: Arc<Mutex<FocusTracker>> = Arc::new(Mutex::new(FocusTracker::new()));
}

#[napi]
pub fn start_tracking() {
    let tracker = TRACKER.clone();
    thread::spawn(move || loop {
        unsafe {
            let hwnd = GetForegroundWindow();
            if hwnd.0 != 0 {
                let mut pid = 0;
                GetWindowThreadProcessId(hwnd, Some(&mut pid));
                if pid != 0 {
                    let mut tracker = tracker.lock().unwrap();
                    tracker.push(pid);
                }
            }
        }
        thread::sleep(Duration::from_millis(100));
    });
}

#[napi]
pub fn get_previous_pids(current_pid: u32) -> Vec<u32> {
    let tracker = TRACKER.lock().unwrap();
    let mut pids = Vec::new();

    // Iterate backwards
    for &pid in tracker.history.iter().rev() {
        if pid != current_pid && !pids.contains(&pid) {
            pids.push(pid);
            if pids.len() >= 5 {
                break;
            }
        }
    }
    pids
}

#[napi]
pub fn get_process_name(pid: u32) -> Option<String> {
    unsafe {
        let handle = OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, false, pid).ok()?;
        let mut buffer = [0u16; 1024];
        let mut size = buffer.len() as u32;

        if QueryFullProcessImageNameW(
            handle,
            PROCESS_NAME_WIN32,
            PWSTR(buffer.as_mut_ptr()),
            &mut size,
        )
        .is_ok()
        {
            use std::ffi::OsString;
            use std::os::windows::ffi::OsStringExt;
            let path = OsString::from_wide(&buffer[..size as usize]);
            if let Ok(full_path) = path.into_string() {
                if let Some(filename) = std::path::Path::new(&full_path).file_name() {
                    return filename.to_str().map(|s| s.to_string());
                }
            }
        }
    }
    None
}

#[napi(object)]
pub struct SearchOptions {
    pub exclude_dirs: Option<Vec<String>>,
}

#[napi]
pub async fn find_file_across_drives(
    target_file_name: String,
    options: Option<SearchOptions>,
) -> Option<String> {
    let disks = Disks::new_with_refreshed_list();
    let base_exclude_paths = vec!["C:\\Windows"];

    let stop_signal = Arc::new(AtomicBool::new(false));

    let exclude_dirs: HashSet<String> = options
        .and_then(|o| o.exclude_dirs)
        .unwrap_or_default()
        .into_iter()
        .map(|s| {
            s.trim_start_matches('/')
                .trim_start_matches('\\')
                .to_string()
        })
        .collect();

    let exclude_dirs_arc = Arc::new(exclude_dirs);

    let drives: Vec<_> = disks
        .iter()
        .map(|disk| disk.mount_point().to_path_buf())
        .filter(|root| {
            let root_str = root.to_string_lossy();
            !base_exclude_paths.iter().any(|ex| root_str.starts_with(ex))
        })
        .collect();

    drives.into_par_iter().find_map_any(|root_path| {
        let exclude_dirs = exclude_dirs_arc.clone();
        for entry in WalkDir::new(root_path)
            .skip_hidden(false)
            .follow_links(false)
            .process_read_dir(move |_depth, _path, _state, children| {
                children.retain(|child| {
                    if let Ok(child) = child {
                        if child.file_type().is_dir() {
                            let name = child.file_name().to_string_lossy();
                            return !exclude_dirs.contains(&name.to_string());
                        }
                    }
                    true
                });
            })
        {
            if stop_signal.load(Ordering::Relaxed) {
                return None;
            }

            if let Ok(entry) = entry {
                if entry.file_name().to_string_lossy() == target_file_name {
                    stop_signal.store(true, Ordering::Relaxed);
                    return Some(entry.path().to_string_lossy().to_string());
                }
            }
        }
        None
    })
}
