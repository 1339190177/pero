//! The same OS file lock excludes local turns and Worker runs on a checkout.
use fs2::FileExt;
use std::{
    fs::{File, OpenOptions},
    path::Path,
};

pub struct WorkspaceLease(File);
impl WorkspaceLease {
    pub fn acquire(workspace: &Path) -> Result<Self, String> {
        let workspace = workspace
            .canonicalize()
            .map_err(|_| "Workspace is unavailable")?;
        if !workspace.is_dir() {
            return Err("Workspace is not a directory".into());
        }
        let mut options = OpenOptions::new();
        options.read(true).write(true).create(true).truncate(false);
        #[cfg(unix)]
        {
            use std::os::unix::fs::OpenOptionsExt;
            options.mode(0o600).custom_flags(libc::O_NOFOLLOW);
        }
        let file = options
            .open(workspace.join(".pero-worker.lock"))
            .map_err(|e| e.to_string())?;
        file.try_lock_exclusive()
            .map_err(|_| "Workspace is busy; a local turn or Worker Run owns its write lock")?;
        Ok(Self(file))
    }
}
impl Drop for WorkspaceLease {
    fn drop(&mut self) {
        let _ = FileExt::unlock(&self.0);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[cfg(unix)]
    #[test]
    fn redirected_lock_is_rejected_without_touching_its_target() {
        let root = std::env::temp_dir().join(format!("pero-lease-{}", uuid::Uuid::new_v4()));
        let workspace = root.join("workspace");
        std::fs::create_dir_all(&workspace).unwrap();
        let target = root.join("outside-lock");
        std::fs::write(&target, "unchanged").unwrap();
        let lock = workspace.join(".pero-worker.lock");
        std::os::unix::fs::symlink(&target, &lock).unwrap();
        assert!(WorkspaceLease::acquire(&workspace).is_err());
        assert_eq!(std::fs::read_to_string(&target).unwrap(), "unchanged");
        std::fs::remove_file(lock).unwrap();
        assert!(WorkspaceLease::acquire(&workspace).is_ok());
        std::fs::remove_dir_all(root).unwrap();
    }

    #[test]
    fn one_checkout_has_one_writer_and_releases_on_drop() {
        let workspace = std::env::temp_dir().join(format!("pero-lease-{}", uuid::Uuid::new_v4()));
        std::fs::create_dir(&workspace).unwrap();
        let lease = WorkspaceLease::acquire(&workspace).unwrap();
        assert!(WorkspaceLease::acquire(&workspace).is_err());
        drop(lease);
        assert!(WorkspaceLease::acquire(&workspace).is_ok());
        std::fs::remove_dir_all(workspace).unwrap();
    }
}
