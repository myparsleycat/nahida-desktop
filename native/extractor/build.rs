fn main() {
    napi_build::setup();

    #[cfg(windows)]
    {
        println!("cargo:rustc-link-lib=xmllite");
        println!("cargo:rustc-link-lib=dylib=advapi32");
        println!("cargo:rustc-link-lib=dylib=crypt32");
        println!("cargo:rustc-link-lib=dylib=user32");
        println!("cargo:rustc-link-lib=dylib=shell32");
    }
}
