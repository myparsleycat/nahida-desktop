#![deny(unsafe_op_in_unsafe_fn)]

mod packed_skinning;

#[unsafe(no_mangle)]
pub extern "C" fn desktop_kernels_abi_version() -> u32 {
    1
}
