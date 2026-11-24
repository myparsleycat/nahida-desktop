#![deny(unsafe_op_in_unsafe_fn)]

mod bc7;
mod bcn;
mod packed_skinning;

#[unsafe(no_mangle)]
pub extern "C" fn desktop_kernels_abi_version() -> u32 {
    1
}
