# `codegen/tonic-build/` — proto codegen for the Rust workspace

Each crate that needs proto bindings will own a small `build.rs` invoking
`tonic_build::configure()` against the shared `../../../../proto/` tree.

Example `build.rs` (drop into `crates/dash-sim/build.rs` when you start it):

```rust
fn main() -> Result<(), Box<dyn std::error::Error>> {
    tonic_build::configure()
        .build_server(true)
        .build_client(true)
        .compile(
            &[
                "../../../../proto/dashsim/v1/dashsim.proto",
            ],
            &["../../../../proto"],
        )?;
    Ok(())
}
```
