use napi_derive::napi;
use std::collections::HashSet;
use windows::core::PWSTR;
use windows::Win32::Foundation::{BOOL, HWND, LPARAM};
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

use std::sync::{Arc, Mutex};
use std::thread;
use std::time::Duration;
use windows::Win32::System::Threading::{
    OpenProcess, QueryFullProcessImageNameW, PROCESS_NAME_WIN32, PROCESS_QUERY_LIMITED_INFORMATION,
};
use windows::Win32::UI::WindowsAndMessaging::GetForegroundWindow;

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
