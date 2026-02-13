#![deny(clippy::all)]

use napi::threadsafe_function::{ThreadsafeFunction, ThreadsafeFunctionCallMode};
use napi_derive::napi;
use std::{
  sync::{
    atomic::{AtomicBool, Ordering},
    Arc,
  },
  thread,
  time::Duration,
};
use windows::{
  core::*,
  Win32::{
    Foundation::{BOOL, HWND, LPARAM, POINT, RECT},
    Graphics::Gdi::ClientToScreen,
    UI::WindowsAndMessaging::{
      EnumWindows, FindWindowW, GetClientRect, GetForegroundWindow, GetWindowThreadProcessId,
      IsWindow, IsWindowVisible,
    },
  },
};

struct FindWindowData {
  pid: u32,
  hwnd: HWND,
}

unsafe extern "system" fn enum_window_proc(hwnd: HWND, lparam: LPARAM) -> BOOL {
  let data = &mut *(lparam.0 as *mut FindWindowData);
  let mut pid = 0;
  GetWindowThreadProcessId(hwnd, Some(&mut pid));
  if pid == data.pid && IsWindowVisible(hwnd).as_bool() {
    data.hwnd = hwnd;
    return BOOL(0);
  }
  BOOL(1)
}

#[napi(object)]
#[derive(Clone, Debug)]
pub struct WindowRect {
  pub x: i32,
  pub y: i32,
  pub width: i32,
  pub height: i32,
}

#[napi(object)]
#[derive(Clone, Debug)]
pub struct NativeOverlayEvent {
  pub event: String,
  pub rect: Option<WindowRect>,
}

#[napi]
pub struct OverlayController {
  stop_flag: Arc<AtomicBool>,
}

#[napi]
impl OverlayController {
  #[napi(constructor)]
  pub fn new() -> Self {
    Self {
      stop_flag: Arc::new(AtomicBool::new(false)),
    }
  }

  #[napi]
  pub fn start(
    &mut self,
    target_title: String,
    callback: ThreadsafeFunction<NativeOverlayEvent>,
  ) -> napi::Result<()> {
    let stop_flag = self.stop_flag.clone();

    stop_flag.store(false, Ordering::SeqCst);

    thread::spawn(move || {
      let mut target_hwnd = HWND(0);
      let mut last_rect = RECT::default();
      let mut last_focus = false;
      let mut attached = false;

      let title_wide: Vec<u16> = target_title
        .encode_utf16()
        .chain(std::iter::once(0))
        .collect();

      loop {
        if stop_flag.load(Ordering::SeqCst) {
          break;
        }

        if !attached {
          unsafe {
            let hwnd = FindWindowW(None, PCWSTR(title_wide.as_ptr()));
            if hwnd.0 != 0 && IsWindow(hwnd).as_bool() {
              target_hwnd = hwnd;
              attached = true;

              let mut client_rect = RECT::default();
              let _ = GetClientRect(target_hwnd, &mut client_rect);
              let mut point = POINT { x: 0, y: 0 };
              let _ = ClientToScreen(target_hwnd, &mut point);

              let width = client_rect.right - client_rect.left;
              let height = client_rect.bottom - client_rect.top;

              let rect = RECT {
                left: point.x,
                top: point.y,
                right: point.x + width,
                bottom: point.y + height,
              };
              last_rect = rect;

              callback.call(
                Ok(NativeOverlayEvent {
                  event: "attach".to_string(),
                  rect: Some(WindowRect {
                    x: rect.left,
                    y: rect.top,
                    width,
                    height,
                  }),
                }),
                ThreadsafeFunctionCallMode::NonBlocking,
              );
            }
          }
        } else {
          unsafe {
            if !IsWindow(target_hwnd).as_bool() {
              attached = false;
              target_hwnd = HWND(0);
              callback.call(
                Ok(NativeOverlayEvent {
                  event: "detach".to_string(),
                  rect: None,
                }),
                ThreadsafeFunctionCallMode::NonBlocking,
              );
              thread::sleep(Duration::from_millis(500));
              continue;
            }

            let mut client_rect = RECT::default();
            let mut point = POINT { x: 0, y: 0 };

            if GetClientRect(target_hwnd, &mut client_rect).is_ok()
              && ClientToScreen(target_hwnd, &mut point).as_bool()
            {
              let rect = RECT {
                left: point.x,
                top: point.y,
                right: point.x + (client_rect.right - client_rect.left),
                bottom: point.y + (client_rect.bottom - client_rect.top),
              };
              let width = rect.right - rect.left;
              let height = rect.bottom - rect.top;
              let last_width = last_rect.right - last_rect.left;
              let last_height = last_rect.bottom - last_rect.top;

              if width != last_width || height != last_height {
                callback.call(
                  Ok(NativeOverlayEvent {
                    event: "resize".to_string(),
                    rect: Some(WindowRect {
                      x: rect.left,
                      y: rect.top,
                      width,
                      height,
                    }),
                  }),
                  ThreadsafeFunctionCallMode::NonBlocking,
                );
                last_rect = rect;
              } else if rect.left != last_rect.left || rect.top != last_rect.top {
                callback.call(
                  Ok(NativeOverlayEvent {
                    event: "move".to_string(),
                    rect: Some(WindowRect {
                      x: rect.left,
                      y: rect.top,
                      width,
                      height,
                    }),
                  }),
                  ThreadsafeFunctionCallMode::NonBlocking,
                );
                last_rect = rect;
              }
            }

            let foreground_hwnd = GetForegroundWindow();
            let is_focused = foreground_hwnd == target_hwnd;

            if is_focused != last_focus {
              if is_focused {
                callback.call(
                  Ok(NativeOverlayEvent {
                    event: "focus".to_string(),
                    rect: None,
                  }),
                  ThreadsafeFunctionCallMode::NonBlocking,
                );
              } else {
                callback.call(
                  Ok(NativeOverlayEvent {
                    event: "blur".to_string(),
                    rect: None,
                  }),
                  ThreadsafeFunctionCallMode::NonBlocking,
                );
              }
              last_focus = is_focused;
            }
          }
        }

        thread::sleep(Duration::from_millis(16));
      }
    });

    Ok(())
  }

  #[napi]
  pub fn start_by_pid(
    &mut self,
    target_pid: u32,
    callback: ThreadsafeFunction<NativeOverlayEvent>,
  ) -> napi::Result<()> {
    let stop_flag = self.stop_flag.clone();

    stop_flag.store(false, Ordering::SeqCst);

    thread::spawn(move || {
      let mut target_hwnd = HWND(0);
      let mut last_rect = RECT::default();
      let mut last_focus = false;
      let mut attached = false;

      loop {
        if stop_flag.load(Ordering::SeqCst) {
          break;
        }

        if !attached {
          unsafe {
            let mut data = FindWindowData {
              pid: target_pid,
              hwnd: HWND(0),
            };
            let _ = EnumWindows(Some(enum_window_proc), LPARAM(&mut data as *mut _ as isize));

            if data.hwnd.0 != 0 {
              target_hwnd = data.hwnd;
              attached = true;

              let mut client_rect = RECT::default();
              let _ = GetClientRect(target_hwnd, &mut client_rect);
              let mut point = POINT { x: 0, y: 0 };
              let _ = ClientToScreen(target_hwnd, &mut point);

              let width = client_rect.right - client_rect.left;
              let height = client_rect.bottom - client_rect.top;

              let rect = RECT {
                left: point.x,
                top: point.y,
                right: point.x + width,
                bottom: point.y + height,
              };
              last_rect = rect;

              callback.call(
                Ok(NativeOverlayEvent {
                  event: "attach".to_string(),
                  rect: Some(WindowRect {
                    x: rect.left,
                    y: rect.top,
                    width,
                    height,
                  }),
                }),
                ThreadsafeFunctionCallMode::NonBlocking,
              );
            }
          }
        } else {
          unsafe {
            if !IsWindow(target_hwnd).as_bool() {
              attached = false;
              target_hwnd = HWND(0);
              callback.call(
                Ok(NativeOverlayEvent {
                  event: "detach".to_string(),
                  rect: None,
                }),
                ThreadsafeFunctionCallMode::NonBlocking,
              );
              thread::sleep(Duration::from_millis(500));
              continue;
            }

            let mut client_rect = RECT::default();
            let mut point = POINT { x: 0, y: 0 };

            if GetClientRect(target_hwnd, &mut client_rect).is_ok()
              && ClientToScreen(target_hwnd, &mut point).as_bool()
            {
              let rect = RECT {
                left: point.x,
                top: point.y,
                right: point.x + (client_rect.right - client_rect.left),
                bottom: point.y + (client_rect.bottom - client_rect.top),
              };
              let width = rect.right - rect.left;
              let height = rect.bottom - rect.top;
              let last_width = last_rect.right - last_rect.left;
              let last_height = last_rect.bottom - last_rect.top;

              if width != last_width || height != last_height {
                callback.call(
                  Ok(NativeOverlayEvent {
                    event: "resize".to_string(),
                    rect: Some(WindowRect {
                      x: rect.left,
                      y: rect.top,
                      width,
                      height,
                    }),
                  }),
                  ThreadsafeFunctionCallMode::NonBlocking,
                );
                last_rect = rect;
              } else if rect.left != last_rect.left || rect.top != last_rect.top {
                callback.call(
                  Ok(NativeOverlayEvent {
                    event: "move".to_string(),
                    rect: Some(WindowRect {
                      x: rect.left,
                      y: rect.top,
                      width,
                      height,
                    }),
                  }),
                  ThreadsafeFunctionCallMode::NonBlocking,
                );
                last_rect = rect;
              }
            }

            let foreground_hwnd = GetForegroundWindow();
            let is_focused = foreground_hwnd == target_hwnd;

            if is_focused != last_focus {
              if is_focused {
                callback.call(
                  Ok(NativeOverlayEvent {
                    event: "focus".to_string(),
                    rect: None,
                  }),
                  ThreadsafeFunctionCallMode::NonBlocking,
                );
              } else {
                callback.call(
                  Ok(NativeOverlayEvent {
                    event: "blur".to_string(),
                    rect: None,
                  }),
                  ThreadsafeFunctionCallMode::NonBlocking,
                );
              }
              last_focus = is_focused;
            }
          }
        }

        thread::sleep(Duration::from_millis(16));
      }
    });

    Ok(())
  }

  #[napi]
  pub fn stop(&self) {
    self.stop_flag.store(true, Ordering::SeqCst);
  }
}
