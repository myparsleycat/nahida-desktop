use serde::{Deserialize, Serialize};

pub const EXPORT_DIRECTORY: usize = 0;
pub const IMPORT_DIRECTORY: usize = 1;
pub const RESOURCE_DIRECTORY: usize = 2;
pub const EXCEPTION_DIRECTORY: usize = 3;
pub const SECURITY_DIRECTORY: usize = 4;
pub const BASERELOC_DIRECTORY: usize = 5;
pub const DEBUG_DIRECTORY: usize = 6;
pub const ARCHITECTURE_DIRECTORY: usize = 7;
pub const GLOBALPTR_DIRECTORY: usize = 8;
pub const TLS_DIRECTORY: usize = 9;
pub const LOAD_CONFIG_DIRECTORY: usize = 10;
pub const BOUND_IMPORT_DIRECTORY: usize = 11;
pub const IAT_DIRECTORY: usize = 12;
pub const DELAY_IMPORT_DIRECTORY: usize = 13;
pub const CLR_DIRECTORY: usize = 14;

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct DataDirectory {
  pub virtual_address: u32,
  pub size: u32,
}

impl DataDirectory {
  pub fn present(&self) -> bool {
    self.virtual_address != 0 && self.size != 0
  }
}

pub fn directory_name(index: usize) -> &'static str {
  match index {
    EXPORT_DIRECTORY => "export",
    IMPORT_DIRECTORY => "import",
    RESOURCE_DIRECTORY => "resource",
    EXCEPTION_DIRECTORY => "exception",
    SECURITY_DIRECTORY => "security/certificate",
    BASERELOC_DIRECTORY => "base relocation",
    DEBUG_DIRECTORY => "debug",
    ARCHITECTURE_DIRECTORY => "architecture",
    GLOBALPTR_DIRECTORY => "global pointer",
    TLS_DIRECTORY => "TLS",
    LOAD_CONFIG_DIRECTORY => "load-config",
    BOUND_IMPORT_DIRECTORY => "bound import",
    IAT_DIRECTORY => "IAT",
    DELAY_IMPORT_DIRECTORY => "delay import",
    CLR_DIRECTORY => "CLR",
    _ => "reserved",
  }
}
