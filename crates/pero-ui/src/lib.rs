#[cfg(any(target_family = "wasm", feature = "native"))]
mod app;
#[cfg_attr(not(any(target_family = "wasm", feature = "native")), allow(dead_code))]
mod apps;
#[cfg(any(target_family = "wasm", feature = "native"))]
mod assets;
#[cfg(any(target_family = "wasm", feature = "native"))]
mod chat;
#[cfg(any(target_family = "wasm", feature = "native"))]
mod components;
#[cfg(any(target_family = "wasm", feature = "native"))]
mod conversation;
#[cfg(any(target_family = "wasm", feature = "native"))]
mod theme;
#[cfg(any(target_family = "wasm", feature = "native"))]
mod view;

#[cfg(any(target_family = "wasm", feature = "native"))]
mod composer;

// Rendered only by the wasm Workbench; native builds compile it for its tests.
#[cfg_attr(not(target_family = "wasm"), allow(dead_code))]
mod cloud;
#[cfg_attr(not(any(target_family = "wasm", feature = "native")), allow(dead_code))]
mod projects;
#[cfg(any(target_family = "wasm", feature = "native"))]
mod project_view;
#[cfg(any(target_family = "wasm", feature = "native"))]
mod local_roadmap;
#[cfg(any(target_family = "wasm", feature = "native"))]
mod settings_menu;
mod stream;
mod tool_ui;

#[cfg(any(target_family = "wasm", feature = "native"))]
mod contributions;
#[cfg(any(target_family = "wasm", feature = "native"))]
mod remote;
#[cfg(any(target_family = "wasm", feature = "native"))]
mod remote_connection;
#[cfg(any(target_family = "wasm", feature = "native"))]
mod remote_view;

#[cfg(all(feature = "native", not(target_family = "wasm")))]
mod desktop_login;
#[cfg(all(feature = "native", not(target_family = "wasm")))]
pub mod native;
#[cfg(all(feature = "native", not(target_family = "wasm")))]
mod cloud_links;
#[cfg(all(feature = "native", not(target_family = "wasm")))]
mod native_cloud;

#[cfg(any(target_family = "wasm", feature = "native", test))]
mod composer_state;
#[cfg(any(target_family = "wasm", feature = "native"))]
mod composer_actions;
#[cfg(any(target_family = "wasm", feature = "native"))]
mod composer_picker;

#[cfg(any(target_family = "wasm", feature = "native"))]
mod session_controls;

#[cfg(any(target_family = "wasm", feature = "native"))]
mod local_execution;

#[cfg(all(feature = "native", not(target_family = "wasm")))]
mod native_secrets;
#[cfg(all(feature = "native", not(target_family = "wasm")))]
mod native_device;
