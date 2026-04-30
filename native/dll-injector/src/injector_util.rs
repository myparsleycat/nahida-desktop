// https://github.com/kubo/injector

use std::ffi::c_void;
use std::mem;
use windows::core::{Error, Result, PCSTR};
use windows::Win32::Foundation::{CloseHandle, FALSE, HANDLE};
use windows::Win32::System::Diagnostics::Debug::{ReadProcessMemory, WriteProcessMemory};
use windows::Win32::System::LibraryLoader::{GetModuleHandleA, GetProcAddress};
use windows::Win32::System::Memory::{
    VirtualAllocEx, VirtualFreeEx, MEM_COMMIT, MEM_RELEASE, MEM_RESERVE, PAGE_EXECUTE_READ,
    PAGE_READWRITE,
};
use windows::Win32::System::Threading::{
    CreateRemoteThread, GetExitCodeThread, OpenProcess, WaitForSingleObject, INFINITE,
    PROCESS_CREATE_THREAD, PROCESS_QUERY_LIMITED_INFORMATION, PROCESS_VM_OPERATION,
    PROCESS_VM_READ, PROCESS_VM_WRITE,
};

const X64_CODE_TEMPLATE: &[u8] = &[
    0x40, 0x53, // push rbx
    0x48, 0x8B, 0xD9, // mov rbx, rcx
    0x48, 0x83, 0xEC, 0x30, // sub rsp, 30h
    0x48, 0x8B, 0x41, 0x30, // mov rax, [rcx+30h] (arg6)
    0x48, 0x89, 0x44, 0x24, 0x28, // mov [rsp+28h], rax
    0x48, 0x8B, 0x41, 0x28, // mov rax, [rcx+28h] (arg5)
    0x48, 0x89, 0x44, 0x24, 0x20, // mov [rsp+20h], rax
    0x4C, 0x8B, 0x49, 0x20, // mov r9, [rcx+20h] (arg4)
    0x4C, 0x8B, 0x41, 0x18, // mov r8, [rcx+18h] (arg3)
    0x48, 0x8B, 0x51, 0x10, // mov rdx, [rcx+10h] (arg2)
    0x48, 0x8B, 0x49, 0x08, // mov rcx, [rcx+8] (arg1)
    0xFF, 0x13, // call [rbx] (func)
    0x48, 0x89, 0x03, // mov [rbx], rax (retval)
    0xFF, 0x15, 0x0A, 0x00, 0x00, 0x00, // call GetLastError (offset patched later)
    0x48, 0x83, 0xC4, 0x30, // add rsp, 30h
    0x5B, // pop rbx
    0xC3, // ret
    0x90, 0x90, 0x90, 0x90, // nop padding
    0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, // Placeholder for GetLastError addr
];

const X64_ADDR_GET_LAST_ERROR: usize = 0x40;

#[repr(C)]
struct RemoteCallArgs {
    func: u64,
    arg1: u64,
    arg2: u64,
    arg3: u64,
    arg4: u64,
    arg5: u64,
    arg6: u64,
}

pub struct Injector {
    process: HANDLE,
    remote_code: *mut c_void,
    remote_data: *mut c_void,
    load_library_addr: u64,
}

impl Injector {
    pub fn attach(pid: u32) -> Result<Self> {
        unsafe {
            let process = OpenProcess(
                PROCESS_QUERY_LIMITED_INFORMATION
                    | PROCESS_CREATE_THREAD
                    | PROCESS_VM_OPERATION
                    | PROCESS_VM_READ
                    | PROCESS_VM_WRITE,
                FALSE,
                pid,
            )?;

            let kernel32 = GetModuleHandleA(PCSTR::from_raw(b"kernel32.dll\0".as_ptr()))?;
            let load_library =
                GetProcAddress(kernel32, PCSTR::from_raw(b"LoadLibraryW\0".as_ptr()));
            let get_last_error =
                GetProcAddress(kernel32, PCSTR::from_raw(b"GetLastError\0".as_ptr()));

            if load_library.is_none() || get_last_error.is_none() {
                return Err(Error::from_win32());
            }

            let page_size = 4096;
            let remote_code = VirtualAllocEx(
                process,
                None,
                page_size,
                MEM_COMMIT | MEM_RESERVE,
                PAGE_EXECUTE_READ,
            );
            if remote_code.is_null() {
                return Err(Error::from_win32());
            }

            let remote_data = VirtualAllocEx(
                process,
                None,
                page_size,
                MEM_COMMIT | MEM_RESERVE,
                PAGE_READWRITE,
            );
            if remote_data.is_null() {
                let _ = VirtualFreeEx(process, remote_code, 0, MEM_RELEASE);
                return Err(Error::from_win32());
            }

            let mut code_buffer = X64_CODE_TEMPLATE.to_vec();
            let get_last_error_addr = get_last_error.unwrap() as u64;

            let addr_bytes = get_last_error_addr.to_ne_bytes();
            for i in 0..8 {
                code_buffer[X64_ADDR_GET_LAST_ERROR + i] = addr_bytes[i];
            }

            let mut written = 0;
            WriteProcessMemory(
                process,
                remote_code,
                code_buffer.as_ptr() as _,
                code_buffer.len(),
                Some(&mut written),
            )?;

            Ok(Injector {
                process,
                remote_code,
                remote_data,
                load_library_addr: load_library.unwrap() as u64,
            })
        }
    }

    pub fn inject(&self, dll_path: &str) -> Result<u64> {
        unsafe {
            let mut path_utf16: Vec<u16> = dll_path.encode_utf16().collect();
            path_utf16.push(0); // null terminator

            let path_len_bytes = path_utf16.len() * 2;

            let args_size = mem::size_of::<RemoteCallArgs>();
            let str_addr = (self.remote_data as u64) + args_size as u64;

            let args = RemoteCallArgs {
                func: self.load_library_addr,
                arg1: str_addr,
                arg2: 0,
                arg3: 0,
                arg4: 0,
                arg5: 0,
                arg6: 0,
            };

            WriteProcessMemory(
                self.process,
                self.remote_data,
                &args as *const _ as *const c_void,
                args_size,
                None,
            )?;

            WriteProcessMemory(
                self.process,
                str_addr as *mut c_void,
                path_utf16.as_ptr() as *const c_void,
                path_len_bytes,
                None,
            )?;

            let thread = CreateRemoteThread(
                self.process,
                None,
                0,
                Some(mem::transmute(self.remote_code)),
                Some(self.remote_data),
                0,
                None,
            )?;

            WaitForSingleObject(thread, INFINITE);

            let mut result: u64 = 0;
            ReadProcessMemory(
                self.process,
                self.remote_data,
                &mut result as *mut _ as *mut c_void,
                8,
                None,
            )?;

            let mut last_error: u32 = 0;
            GetExitCodeThread(thread, &mut last_error)?;
            CloseHandle(thread)?;

            if result == 0 {
                return Err(Error::from_win32());
            }

            Ok(result)
        }
    }
}

impl Drop for Injector {
    fn drop(&mut self) {
        unsafe {
            if !self.remote_code.is_null() {
                let _ = VirtualFreeEx(self.process, self.remote_code, 0, MEM_RELEASE);
            }
            if !self.remote_data.is_null() {
                let _ = VirtualFreeEx(self.process, self.remote_data, 0, MEM_RELEASE);
            }
            if !self.process.is_invalid() {
                CloseHandle(self.process).ok();
            }
        }
    }
}
