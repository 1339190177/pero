//! Prototype-only data; these are not Host sessions or remote Task/Run results.
pub(crate) struct ExampleTask {
    pub title_key: &'static str,
    pub repository: &'static str,
    pub status_key: &'static str,
    pub age_key: &'static str,
    pub note_key: &'static str,
    pub badge_kind: &'static str,
    pub ready: bool,
}
pub(crate) const TASKS: [ExampleTask; 5] = [
    ExampleTask {
        title_key: "task.order",
        repository: "order-service",
        status_key: "status.review",
        age_key: "time.eight",
        note_key: "",
        badge_kind: "success",
        ready: true,
    },
    ExampleTask {
        title_key: "task.export",
        repository: "api-gateway",
        status_key: "status.testing",
        age_key: "time.twelve",
        note_key: "task.testing",
        badge_kind: "info",
        ready: false,
    },
    ExampleTask {
        title_key: "task.search",
        repository: "search-service",
        status_key: "status.coding",
        age_key: "time.twenty_eight",
        note_key: "task.coding",
        badge_kind: "info",
        ready: false,
    },
    ExampleTask {
        title_key: "task.errors",
        repository: "core-service",
        status_key: "status.blocked",
        age_key: "time.hour",
        note_key: "task.needs_input",
        badge_kind: "warning",
        ready: false,
    },
    ExampleTask {
        title_key: "task.dependencies",
        repository: "web-console",
        status_key: "status.queued",
        age_key: "time.two_hours",
        note_key: "task.queued",
        badge_kind: "muted",
        ready: false,
    },
];

pub(crate) const CHECKS: [(&str, &str); 3] = [
    ("overview.typecheck", "status.passed"),
    ("overview.tests", "overview.tests_passed"),
    ("overview.build", "status.passed"),
];

pub(crate) const CHANGED_FILES: [(&str, &str, &str); 3] = [
    ("src/orders/cancel.ts", "+24", "−8"),
    ("src/orders/cancel.test.ts", "+21", "−0"),
    ("src/orders/types.ts", "+2", "−4"),
];

pub(crate) const EXECUTION_STEPS: [(&str, &str, &str); 4] = [
    ("overview.prepare", "3", "10:12"),
    ("overview.code", "6", "10:15"),
    ("overview.verify", "4", "10:21"),
    ("overview.pr", "2", "10:23"),
];
