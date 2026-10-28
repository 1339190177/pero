#[cfg(target_family = "wasm")]
mod app;
#[cfg(target_family = "wasm")]
mod assets;
#[cfg(target_family = "wasm")]
mod chat;
#[cfg(target_family = "wasm")]
mod components;
#[cfg(target_family = "wasm")]
mod conversation;
#[cfg(target_family = "wasm")]
mod theme;
#[cfg(target_family = "wasm")]
mod view;

#[cfg(target_family = "wasm")]
mod composer;

// Rendered only by the wasm Workbench; native builds compile it for its tests.
#[cfg_attr(not(target_family = "wasm"), allow(dead_code))]
mod cloud;
mod stream;
mod tool_ui;

#[cfg(target_family = "wasm")]
mod remote;
#[cfg(target_family = "wasm")]
mod remote_connection;
