package todos

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/phodal/pero/harness/go/harness"
	point "github.com/phodal/pero/harness/go/runtime"
)

// Frozen 55-call public source matrix, revision 2fbf4359b82fb94c03dcce0b831660d87f12ea5c.
// Evidence manifest: 1f5d5bd9d34ce0c011d6b2babc9a57ce444ef5601bfd7a7cd383ece8e20056a2.
// matrix.json: d856d7aae29db22bf30b7504fe97151ce5138ea494034255f1194d646076fbd4.
// Native baseline: Stage34 950f678e70ef501caf0fbd142501e35ef1dc17ec; input freeze
// 4bf26570375e6c5b0fbf281cee21165b3b794a075765242240c8ca90af0d03f8.
// Representation residuals below deliberately assert native behavior, not whole-row
// source parity. No source files or runtime fixture are needed to run these tests.
type todoNestedSourceCase struct {
	id                                     string
	parallel                               bool
	category, raw, message, value, content string
}

var todoNestedSourceCases = []todoNestedSourceCase{
	{"seed-normalized-order-counts", false, "success", "{\"todos\":[{\"content\":\"\ufeff z \ufeff\",\"status\":\"completed\"},{\"content\":\" a \",\"status\":\"in_progress\"},{\"content\":\" b \",\"status\":\"pending\"},{\"content\":\"c\",\"status\":\"completed\"}]}", "", "{\"todos\":[{\"content\":\"z\",\"status\":\"completed\"},{\"content\":\"a\",\"status\":\"in_progress\"},{\"content\":\"b\",\"status\":\"pending\"},{\"content\":\"c\",\"status\":\"completed\"}],\"counts\":{\"pending\":1,\"inProgress\":1,\"completed\":2}}", "Updated todo list: 1 pending, 1 in progress, 2 completed."},
	{"root-null", false, "schema", "null", "invalid arguments: \"arguments\" must be an object", "", "Error: invalid arguments: \"arguments\" must be an object"},
	{"root-array", false, "schema", "[]", "invalid arguments: \"arguments\" must be an object", "", "Error: invalid arguments: \"arguments\" must be an object"},
	{"root-missing-known-with-extras", false, "schema", "{\"TODOS\":[],\"extra\":true}", "invalid arguments: missing required property \"todos\"", "", "Error: invalid arguments: missing required property \"todos\""},
	{"list-null-with-root-extras", false, "schema", "{\"extra\":{\"content\":null},\"todos\":null,\"TODOS\":[]}", "invalid arguments: \"todos\" must be an array", "", "Error: invalid arguments: \"todos\" must be an array"},
	{"list-string-with-root-extras", false, "schema", "{\"extra\":{\"content\":null},\"todos\":\"x\",\"TODOS\":[]}", "invalid arguments: \"todos\" must be an array", "", "Error: invalid arguments: \"todos\" must be an array"},
	{"list-object-with-root-extras", false, "schema", "{\"extra\":{\"content\":null},\"todos\":{\"content\":null},\"TODOS\":[]}", "invalid arguments: \"todos\" must be an array", "", "Error: invalid arguments: \"todos\" must be an array"},
	{"list-number-with-root-extras", false, "schema", "{\"extra\":{\"content\":null},\"todos\":1,\"TODOS\":[]}", "invalid arguments: \"todos\" must be an array", "", "Error: invalid arguments: \"todos\" must be an array"},
	{"list-boolean-with-root-extras", false, "schema", "{\"extra\":{\"content\":null},\"todos\":false,\"TODOS\":[]}", "invalid arguments: \"todos\" must be an array", "", "Error: invalid arguments: \"todos\" must be an array"},
	{"item-nonobjects-index-order", false, "schema", "{\"todos\":[null,[],false,1,\"x\"]}", "invalid arguments: \"todos[0]\" must be an object; \"todos[1]\" must be an object; \"todos[2]\" must be an object; \"todos[3]\" must be an object; \"todos[4]\" must be an object", "", "Error: invalid arguments: \"todos[0]\" must be an object; \"todos[1]\" must be an object; \"todos[2]\" must be an object; \"todos[3]\" must be an object; \"todos[4]\" must be an object"},
	{"item-missing-both", false, "schema", "{\"todos\":[{}]}", "invalid arguments: missing required property \"todos[0].content\"; missing required property \"todos[0].status\"", "", "Error: invalid arguments: missing required property \"todos[0].content\"; missing required property \"todos[0].status\""},
	{"missing-content-invalid-status-extras", false, "schema", "{\"todos\":[{\"zzz\":1,\"status\":\"doing\",\"aaa\":2}]}", "invalid arguments: missing required property \"todos[0].content\"; \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[0].zzz\" is not a declared property (additionalProperties: false); \"todos[0].aaa\" is not a declared property (additionalProperties: false)", "", "Error: invalid arguments: missing required property \"todos[0].content\"; \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[0].zzz\" is not a declared property (additionalProperties: false); \"todos[0].aaa\" is not a declared property (additionalProperties: false)"},
	{"missing-status-invalid-content-extras", false, "schema", "{\"todos\":[{\"zzz\":1,\"content\":null,\"aaa\":2}]}", "invalid arguments: missing required property \"todos[0].status\"; \"todos[0].content\" must be a string; \"todos[0].zzz\" is not a declared property (additionalProperties: false); \"todos[0].aaa\" is not a declared property (additionalProperties: false)", "", "Error: invalid arguments: missing required property \"todos[0].status\"; \"todos[0].content\" must be a string; \"todos[0].zzz\" is not a declared property (additionalProperties: false); \"todos[0].aaa\" is not a declared property (additionalProperties: false)"},
	{"missing-both-uppercase-a", false, "schema", "{\"todos\":[{\"STATUS\":\"pending\",\"CONTENT\":\"x\"}]}", "invalid arguments: missing required property \"todos[0].content\"; missing required property \"todos[0].status\"; \"todos[0].STATUS\" is not a declared property (additionalProperties: false); \"todos[0].CONTENT\" is not a declared property (additionalProperties: false)", "", "Error: invalid arguments: missing required property \"todos[0].content\"; missing required property \"todos[0].status\"; \"todos[0].STATUS\" is not a declared property (additionalProperties: false); \"todos[0].CONTENT\" is not a declared property (additionalProperties: false)"},
	{"missing-both-uppercase-b", false, "schema", "{\"todos\":[{\"CONTENT\":\"x\",\"STATUS\":\"pending\"}]}", "invalid arguments: missing required property \"todos[0].content\"; missing required property \"todos[0].status\"; \"todos[0].CONTENT\" is not a declared property (additionalProperties: false); \"todos[0].STATUS\" is not a declared property (additionalProperties: false)", "", "Error: invalid arguments: missing required property \"todos[0].content\"; missing required property \"todos[0].status\"; \"todos[0].CONTENT\" is not a declared property (additionalProperties: false); \"todos[0].STATUS\" is not a declared property (additionalProperties: false)"},
	{"known-and-unknown-order-a", false, "schema", "{\"todos\":[{\"content\":null,\"status\":\"doing\",\"zzz\":1,\"aaa\":2}]}", "invalid arguments: \"todos[0].content\" must be a string; \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[0].zzz\" is not a declared property (additionalProperties: false); \"todos[0].aaa\" is not a declared property (additionalProperties: false)", "", "Error: invalid arguments: \"todos[0].content\" must be a string; \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[0].zzz\" is not a declared property (additionalProperties: false); \"todos[0].aaa\" is not a declared property (additionalProperties: false)"},
	{"known-and-unknown-order-b", false, "schema", "{\"todos\":[{\"zzz\":1,\"status\":\"doing\",\"aaa\":2,\"content\":null}]}", "invalid arguments: \"todos[0].content\" must be a string; \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[0].zzz\" is not a declared property (additionalProperties: false); \"todos[0].aaa\" is not a declared property (additionalProperties: false)", "", "Error: invalid arguments: \"todos[0].content\" must be a string; \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[0].zzz\" is not a declared property (additionalProperties: false); \"todos[0].aaa\" is not a declared property (additionalProperties: false)"},
	{"known-and-unknown-order-c", false, "schema", "{\"todos\":[{\"aaa\":2,\"status\":\"doing\",\"zzz\":1,\"content\":null}]}", "invalid arguments: \"todos[0].content\" must be a string; \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[0].aaa\" is not a declared property (additionalProperties: false); \"todos[0].zzz\" is not a declared property (additionalProperties: false)", "", "Error: invalid arguments: \"todos[0].content\" must be a string; \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[0].aaa\" is not a declared property (additionalProperties: false); \"todos[0].zzz\" is not a declared property (additionalProperties: false)"},
	{"missing-first-despite-input-order", false, "schema", "{\"todos\":[{\"content\":null,\"aaa\":2,\"zzz\":1}]}", "invalid arguments: missing required property \"todos[0].status\"; \"todos[0].content\" must be a string; \"todos[0].aaa\" is not a declared property (additionalProperties: false); \"todos[0].zzz\" is not a declared property (additionalProperties: false)", "", "Error: invalid arguments: missing required property \"todos[0].status\"; \"todos[0].content\" must be a string; \"todos[0].aaa\" is not a declared property (additionalProperties: false); \"todos[0].zzz\" is not a declared property (additionalProperties: false)"},
	{"both-field-types-index-order", false, "schema", "{\"todos\":[{\"status\":null,\"content\":null},{\"status\":false,\"content\":false},{\"status\":1,\"content\":1},{\"status\":[],\"content\":[]},{\"status\":{},\"content\":{}}]}", "invalid arguments: \"todos[0].content\" must be a string; \"todos[0].status\" must be a string; \"todos[1].content\" must be a string; \"todos[1].status\" must be a string; \"todos[2].content\" must be a string; \"todos[2].status\" must be a string; \"todos[3].content\" must be a string; \"todos[3].status\" must be a string; \"todos[4].content\" must be a string; \"todos[4].status\" must be a string", "", "Error: invalid arguments: \"todos[0].content\" must be a string; \"todos[0].status\" must be a string; \"todos[1].content\" must be a string; \"todos[1].status\" must be a string; \"todos[2].content\" must be a string; \"todos[2].status\" must be a string; \"todos[3].content\" must be a string; \"todos[3].status\" must be a string; \"todos[4].content\" must be a string; \"todos[4].status\" must be a string"},
	{"status-enum-no-normalization", false, "schema", "{\"todos\":[{\"content\":\"a\",\"status\":\" pending \"},{\"content\":\"b\",\"status\":\"PENDING\"}]}", "invalid arguments: \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[1].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]", "", "Error: invalid arguments: \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[1].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]"},
	{"unknown-value-not-schema-walked", false, "schema", "{\"todos\":[{\"content\":\"x\",\"status\":\"pending\",\"extra\":{\"content\":null,\"status\":null,\"todos\":[null]}}]}", "invalid arguments: \"todos[0].extra\" is not a declared property (additionalProperties: false)", "", "Error: invalid arguments: \"todos[0].extra\" is not a declared property (additionalProperties: false)"},
	{"unknown-prototype-names-are-own", false, "schema", "{\"todos\":[{\"__proto__\":{\"content\":\"forged\"},\"constructor\":1,\"toString\":true,\"hasOwnProperty\":false,\"content\":\"x\",\"status\":\"pending\"}]}", "invalid arguments: \"todos[0].__proto__\" is not a declared property (additionalProperties: false); \"todos[0].constructor\" is not a declared property (additionalProperties: false); \"todos[0].toString\" is not a declared property (additionalProperties: false); \"todos[0].hasOwnProperty\" is not a declared property (additionalProperties: false)", "", "Error: invalid arguments: \"todos[0].__proto__\" is not a declared property (additionalProperties: false); \"todos[0].constructor\" is not a declared property (additionalProperties: false); \"todos[0].toString\" is not a declared property (additionalProperties: false); \"todos[0].hasOwnProperty\" is not a declared property (additionalProperties: false)"},
	{"integer-unknown-keys-a", false, "schema", "{\"todos\":[{\"10\":1,\"2\":1,\"02\":1,\"zzz\":1,\"aaa\":1,\"content\":null,\"status\":\"doing\"}]}", "invalid arguments: \"todos[0].content\" must be a string; \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[0].2\" is not a declared property (additionalProperties: false); \"todos[0].10\" is not a declared property (additionalProperties: false); \"todos[0].02\" is not a declared property (additionalProperties: false); \"todos[0].zzz\" is not a declared property (additionalProperties: false); \"todos[0].aaa\" is not a declared property (additionalProperties: false)", "", "Error: invalid arguments: \"todos[0].content\" must be a string; \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[0].2\" is not a declared property (additionalProperties: false); \"todos[0].10\" is not a declared property (additionalProperties: false); \"todos[0].02\" is not a declared property (additionalProperties: false); \"todos[0].zzz\" is not a declared property (additionalProperties: false); \"todos[0].aaa\" is not a declared property (additionalProperties: false)"},
	{"integer-unknown-keys-b", false, "schema", "{\"todos\":[{\"aaa\":1,\"10\":1,\"zzz\":1,\"2\":1,\"02\":1,\"status\":\"doing\",\"content\":null}]}", "invalid arguments: \"todos[0].content\" must be a string; \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[0].2\" is not a declared property (additionalProperties: false); \"todos[0].10\" is not a declared property (additionalProperties: false); \"todos[0].aaa\" is not a declared property (additionalProperties: false); \"todos[0].zzz\" is not a declared property (additionalProperties: false); \"todos[0].02\" is not a declared property (additionalProperties: false)", "", "Error: invalid arguments: \"todos[0].content\" must be a string; \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[0].2\" is not a declared property (additionalProperties: false); \"todos[0].10\" is not a declared property (additionalProperties: false); \"todos[0].aaa\" is not a declared property (additionalProperties: false); \"todos[0].zzz\" is not a declared property (additionalProperties: false); \"todos[0].02\" is not a declared property (additionalProperties: false)"},
	{"integer-index-endpoints-and-noncanonical-keys", false, "schema", "{\"todos\":[{\"4294967295\":1,\"4294967294\":1,\"00\":1,\"0\":1,\"-0\":1,\"1.0\":1,\"1e0\":1,\"01\":1,\"1\":1,\"content\":null,\"status\":\"doing\"}]}", "invalid arguments: \"todos[0].content\" must be a string; \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[0].0\" is not a declared property (additionalProperties: false); \"todos[0].1\" is not a declared property (additionalProperties: false); \"todos[0].4294967294\" is not a declared property (additionalProperties: false); \"todos[0].4294967295\" is not a declared property (additionalProperties: false); \"todos[0].00\" is not a declared property (additionalProperties: false); \"todos[0].-0\" is not a declared property (additionalProperties: false); \"todos[0].1.0\" is not a declared property (additionalProperties: false); \"todos[0].1e0\" is not a declared property (additionalProperties: false); \"todos[0].01\" is not a declared property (additionalProperties: false)", "", "Error: invalid arguments: \"todos[0].content\" must be a string; \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[0].0\" is not a declared property (additionalProperties: false); \"todos[0].1\" is not a declared property (additionalProperties: false); \"todos[0].4294967294\" is not a declared property (additionalProperties: false); \"todos[0].4294967295\" is not a declared property (additionalProperties: false); \"todos[0].00\" is not a declared property (additionalProperties: false); \"todos[0].-0\" is not a declared property (additionalProperties: false); \"todos[0].1.0\" is not a declared property (additionalProperties: false); \"todos[0].1e0\" is not a declared property (additionalProperties: false); \"todos[0].01\" is not a declared property (additionalProperties: false)"},
	{"duplicate-keys-last-value-first-position", false, "schema", "{\"todos\":[{\"zzz\":1,\"content\":\"first valid\",\"aaa\":2,\"status\":\"pending\",\"zzz\":3,\"content\":null,\"status\":\"doing\"}]}", "invalid arguments: \"todos[0].content\" must be a string; \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[0].zzz\" is not a declared property (additionalProperties: false); \"todos[0].aaa\" is not a declared property (additionalProperties: false)", "", "Error: invalid arguments: \"todos[0].content\" must be a string; \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[0].zzz\" is not a declared property (additionalProperties: false); \"todos[0].aaa\" is not a declared property (additionalProperties: false)"},
	{"escaped-equivalent-keys-last-value-first-position", false, "schema", "{\"todos\":[{\"z\\u007az\":1,\"\\u0063ontent\":\"first valid\",\"aaa\":2,\"stat\\u0075s\":\"pending\",\"zzz\":3,\"content\":null,\"status\":\"doing\"}]}", "invalid arguments: \"todos[0].content\" must be a string; \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[0].zzz\" is not a declared property (additionalProperties: false); \"todos[0].aaa\" is not a declared property (additionalProperties: false)", "", "Error: invalid arguments: \"todos[0].content\" must be a string; \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[0].zzz\" is not a declared property (additionalProperties: false); \"todos[0].aaa\" is not a declared property (additionalProperties: false)"},
	{"duplicate-known-and-escaped-root-last-valid", false, "success", "{\"todos\":[null],\"to\\u0064os\":[{\"content\":null,\"status\":\"doing\",\"\\u0063ontent\":\" kept \",\"stat\\u0075s\":\"completed\"}]}", "", "{\"todos\":[{\"content\":\"kept\",\"status\":\"completed\"}],\"counts\":{\"pending\":0,\"inProgress\":0,\"completed\":1}}", "Updated todo list: 0 pending, 0 in progress, 1 completed."},
	{"unicode-unknown-keys-insertion-order", false, "schema", "{\"todos\":[{\"\\u00e9\":1,\"z\":2,\"e\\u0301\":3,\"\\ud83d\\ude00\":4,\"content\":\"x\",\"status\":\"pending\"}]}", "invalid arguments: \"todos[0].é\" is not a declared property (additionalProperties: false); \"todos[0].z\" is not a declared property (additionalProperties: false); \"todos[0].é\" is not a declared property (additionalProperties: false); \"todos[0].😀\" is not a declared property (additionalProperties: false)", "", "Error: invalid arguments: \"todos[0].é\" is not a declared property (additionalProperties: false); \"todos[0].z\" is not a declared property (additionalProperties: false); \"todos[0].é\" is not a declared property (additionalProperties: false); \"todos[0].😀\" is not a declared property (additionalProperties: false)"},
	{"array-index-2-before-10", false, "schema", "{\"todos\":[{\"content\":\"item-0\",\"status\":\"pending\"},{\"content\":\"item-1\",\"status\":\"pending\"},{\"content\":\"item-2\"},{\"content\":\"item-3\",\"status\":\"pending\"},{\"content\":\"item-4\",\"status\":\"pending\"},{\"content\":\"item-5\",\"status\":\"pending\"},{\"content\":\"item-6\",\"status\":\"pending\"},{\"content\":\"item-7\",\"status\":\"pending\"},{\"content\":\"item-8\",\"status\":\"pending\"},{\"content\":\"item-9\",\"status\":\"pending\"},{\"status\":\"doing\"},{\"content\":\"item-11\",\"status\":\"pending\"}]}", "invalid arguments: missing required property \"todos[2].status\"; missing required property \"todos[10].content\"; \"todos[10].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]", "", "Error: invalid arguments: missing required property \"todos[2].status\"; missing required property \"todos[10].content\"; \"todos[10].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]"},
	{"schema-after-blank", false, "schema", "{\"todos\":[{\"content\":\" \ufeff \",\"status\":\"pending\"},{}]}", "invalid arguments: missing required property \"todos[1].content\"; missing required property \"todos[1].status\"", "", "Error: invalid arguments: missing required property \"todos[1].content\"; missing required property \"todos[1].status\""},
	{"schema-after-duplicate", false, "schema", "{\"todos\":[{\"content\":\" x \",\"status\":\"pending\"},{\"content\":\"x\",\"status\":\"pending\"},{\"content\":\"y\",\"status\":\"doing\",\"extra\":1}]}", "invalid arguments: \"todos[2].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[2].extra\" is not a declared property (additionalProperties: false)", "", "Error: invalid arguments: \"todos[2].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[2].extra\" is not a declared property (additionalProperties: false)"},
	{"schema-before-blank", false, "schema", "{\"todos\":[{\"content\":\"a\",\"status\":\"doing\"},{\"content\":\" \ufeff \",\"status\":\"pending\"}]}", "invalid arguments: \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]", "", "Error: invalid arguments: \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]"},
	{"schema-after-active-overflow", false, "schema", "{\"todos\":[{\"content\":\"a\",\"status\":\"in_progress\"},{\"content\":\"b\",\"status\":\"in_progress\"},null]}", "invalid arguments: \"todos[2]\" must be an object", "", "Error: invalid arguments: \"todos[2]\" must be an object"},
	{"all-item-errors-before-next-index", false, "schema", "{\"todos\":[{\"zzz\":1},{\"aaa\":2,\"status\":\"doing\",\"content\":null},null]}", "invalid arguments: missing required property \"todos[0].content\"; missing required property \"todos[0].status\"; \"todos[0].zzz\" is not a declared property (additionalProperties: false); \"todos[1].content\" must be a string; \"todos[1].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[1].aaa\" is not a declared property (additionalProperties: false); \"todos[2]\" must be an object", "", "Error: invalid arguments: missing required property \"todos[0].content\"; missing required property \"todos[0].status\"; \"todos[0].zzz\" is not a declared property (additionalProperties: false); \"todos[1].content\" must be a string; \"todos[1].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[1].aaa\" is not a declared property (additionalProperties: false); \"todos[2]\" must be an object"},
	{"all-item-errors-reversed-items", false, "schema", "{\"todos\":[null,{\"aaa\":2,\"status\":\"doing\",\"content\":null},{\"zzz\":1}]}", "invalid arguments: \"todos[0]\" must be an object; \"todos[1].content\" must be a string; \"todos[1].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[1].aaa\" is not a declared property (additionalProperties: false); missing required property \"todos[2].content\"; missing required property \"todos[2].status\"; \"todos[2].zzz\" is not a declared property (additionalProperties: false)", "", "Error: invalid arguments: \"todos[0]\" must be an object; \"todos[1].content\" must be a string; \"todos[1].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[1].aaa\" is not a declared property (additionalProperties: false); missing required property \"todos[2].content\"; missing required property \"todos[2].status\"; \"todos[2].zzz\" is not a declared property (additionalProperties: false)"},
	{"unknown-diagnostic-path-is-literal", false, "schema", "{\"todos\":[{\"content\":\"x\",\"status\":\"pending\",\"q\\\".x[0]\\n\":true}]}", "invalid arguments: \"todos[0].q\".x[0]\n\" is not a declared property (additionalProperties: false)", "", "Error: invalid arguments: \"todos[0].q\".x[0]\n\" is not a declared property (additionalProperties: false)"},
	{"root-extra-order-a", false, "success", "{\"TODOS\":[],\"normalization\":\"legacy\",\"extra\":{\"content\":null},\"todos\":[{\"content\":\"\ufeff keep \ufeff\",\"status\":\"pending\"},{\"content\":\" \ufeff \",\"status\":\"pending\"}]}", "", "{\"todos\":[{\"content\":\"keep\",\"status\":\"pending\"},{\"content\":\"\",\"status\":\"pending\"}],\"counts\":{\"pending\":2,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 2 pending, 0 in progress, 0 completed."},
	{"root-extra-order-b", false, "success", "{\"todos\":[{\"content\":\"\ufeff keep \ufeff\",\"status\":\"pending\"},{\"content\":\" \ufeff \",\"status\":\"pending\"}],\"extra\":{\"content\":null},\"normalization\":\"legacy\",\"TODOS\":[]}", "", "{\"todos\":[{\"content\":\"keep\",\"status\":\"pending\"},{\"content\":\"\",\"status\":\"pending\"}],\"counts\":{\"pending\":2,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 2 pending, 0 in progress, 0 completed."},
	{"root-prototype-name-is-ignored", false, "success", "{\"__proto__\":{\"todos\":[null]},\"todos\":[{\"content\":\" kept \",\"status\":\"completed\"}]}", "", "{\"todos\":[{\"content\":\"kept\",\"status\":\"completed\"}],\"counts\":{\"pending\":0,\"inProgress\":0,\"completed\":1}}", "Updated todo list: 0 pending, 0 in progress, 1 completed."},
	{"domain-blank", false, "domain", "{\"todos\":[{\"content\":\" \ufeff \",\"status\":\"pending\"}]}", "invalid todo: `content` must be a non-empty string", "", "Error: invalid todo: `content` must be a non-empty string"},
	{"domain-duplicate", false, "domain", "{\"todos\":[{\"content\":\" x \",\"status\":\"pending\"},{\"content\":\"x\",\"status\":\"completed\"}]}", "invalid todos: duplicate content \"x\"", "", "Error: invalid todos: duplicate content \"x\""},
	{"domain-active-cap", false, "domain", "{\"todos\":[{\"content\":\"a\",\"status\":\"in_progress\"},{\"content\":\"b\",\"status\":\"in_progress\"}]}", "invalid todos: at most one task may be in_progress (got 2)", "", "Error: invalid todos: at most one task may be in_progress (got 2)"},
	{"domain-blank-before-cap", false, "domain", "{\"todos\":[{\"content\":\"a\",\"status\":\"in_progress\"},{\"content\":\"b\",\"status\":\"in_progress\"},{\"content\":\"\ufeff\",\"status\":\"pending\"}]}", "invalid todo: `content` must be a non-empty string", "", "Error: invalid todo: `content` must be a non-empty string"},
	{"domain-duplicate-before-cap", false, "domain", "{\"todos\":[{\"content\":\"x\",\"status\":\"in_progress\"},{\"content\":\"b\",\"status\":\"in_progress\"},{\"content\":\" x \",\"status\":\"pending\"}]}", "invalid todos: duplicate content \"x\"", "", "Error: invalid todos: duplicate content \"x\""},
	{"residual-negative-zero-root-extra", false, "lossless-precheck-residual", "{\"todos\":[],\"extra\":-0}", "", "", ""},
	{"residual-negative-zero-preempts-schema", false, "lossless-precheck-residual", "{\"todos\":[{\"content\":null,\"status\":\"doing\",\"extra\":1}],\"ignored\":-0}", "", "", ""},
	{"residual-lone-surrogate", false, "representation-residual-success", "{\"todos\":[{\"content\":\"\\ud800\",\"status\":\"pending\"}]}", "", "", ""},
	{"residual-lone-surrogate-unknown-key", false, "representation-residual-schema", "{\"todos\":[{\"\\ud800\":1,\"content\":null,\"status\":\"doing\"}]}", "", "", ""},
	{"replace-empty-control", false, "success", "{\"todos\":[]}", "", "{\"todos\":[],\"counts\":{\"pending\":0,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 0 pending, 0 in progress, 0 completed."},
	{"known-and-unknown-order-b", true, "schema", "{\"todos\":[{\"zzz\":1,\"status\":\"doing\",\"aaa\":2,\"content\":null}]}", "invalid arguments: \"todos[0].content\" must be a string; \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[0].zzz\" is not a declared property (additionalProperties: false); \"todos[0].aaa\" is not a declared property (additionalProperties: false)", "", "Error: invalid arguments: \"todos[0].content\" must be a string; \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]; \"todos[0].zzz\" is not a declared property (additionalProperties: false); \"todos[0].aaa\" is not a declared property (additionalProperties: false)"},
	{"schema-after-active-overflow", true, "schema", "{\"todos\":[{\"content\":\"a\",\"status\":\"in_progress\"},{\"content\":\"b\",\"status\":\"in_progress\"},null]}", "invalid arguments: \"todos[2]\" must be an object", "", "Error: invalid arguments: \"todos[2]\" must be an object"},
	{"domain-active-cap", true, "success-parallel-policy", "{\"todos\":[{\"content\":\"a\",\"status\":\"in_progress\"},{\"content\":\"b\",\"status\":\"in_progress\"}]}", "", "{\"todos\":[{\"content\":\"a\",\"status\":\"in_progress\"},{\"content\":\"b\",\"status\":\"in_progress\"}],\"counts\":{\"pending\":0,\"inProgress\":2,\"completed\":0}}", "Updated todo list: 0 pending, 2 in progress, 0 completed."},
	{"domain-blank-before-cap", true, "domain", "{\"todos\":[{\"content\":\"a\",\"status\":\"in_progress\"},{\"content\":\"b\",\"status\":\"in_progress\"},{\"content\":\"\ufeff\",\"status\":\"pending\"}]}", "invalid todo: `content` must be a non-empty string", "", "Error: invalid todo: `content` must be a non-empty string"},
}

type todoNestedCounters struct{ before, after, body, reason, approval, guard int }

func (c todoNestedCounters) minus(before todoNestedCounters) todoNestedCounters {
	return todoNestedCounters{c.before - before.before, c.after - before.after, c.body - before.body, c.reason - before.reason, c.approval - before.approval, c.guard - before.guard}
}

func todoNestedFixture(t *testing.T, mode harness.ToolArgumentErrorMode, normalization ContentNormalizationMode, parallel bool) (*harness.Harness, *harness.Agent, *todoNestedCounters) {
	t.Helper()
	h, _ := todoDomainHarness(t)
	counters := new(todoNestedCounters)
	if _, err := Mount(t.Context(), h, Options{
		ArgumentErrorMode: mode, ContentNormalization: normalization,
		AllowAdditionalInputProperties: true, AllowParallelInProgress: parallel,
		ToolWrapper: func(tool harness.Tool) harness.Tool {
			if tool.ArgumentErrorMode != mode {
				t.Errorf("option not installed before trusted wrapper: %d", tool.ArgumentErrorMode)
			}
			original := tool.ExecuteRequest
			tool.ExecuteRequest = func(ctx context.Context, request harness.ToolRequest) (harness.ToolResult, error) {
				counters.body++
				return original(ctx, request)
			}
			tool.RequiresApproval = true
			tool.ApprovalReason = func(harness.ToolRequest) string { counters.reason++; return "synthetic Todo diagnostics" }
			return tool
		},
	}); err != nil {
		t.Fatal(err)
	}
	agent, err := h.Agents().Create(t.Context(), harness.AgentOptions{ID: "todo-nested", Configure: func(_ context.Context, run *point.Run, _ *harness.Agent) error {
		if _, err := h.Tools().Before.Use(run, func(_ context.Context, request harness.ToolRequest, next point.Next[harness.ToolRequest]) (harness.ToolRequest, error) {
			counters.before++
			return next(request)
		}); err != nil {
			return err
		}
		if _, err := h.Tools().After.Use(run, func(_ context.Context, out harness.ToolOutcome, next point.Next[harness.ToolOutcome]) (harness.ToolOutcome, error) {
			counters.after++
			return next(out)
		}); err != nil {
			return err
		}
		if _, err := h.Tools().Guard(run, func(context.Context, harness.ToolRequest) error { counters.guard++; return nil }); err != nil {
			return err
		}
		_, err := h.Approvals().Register(run, func(context.Context, harness.ApprovalRequest) (harness.ApprovalOutcome, error) {
			counters.approval++
			return harness.ApprovalAllowedOnce, nil
		})
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Session().Append("turn/start", map[string]int{"turn": 1}); err != nil {
		t.Fatal(err)
	}
	return h, agent, counters
}

func todoNestedAssertDiagnostic(t *testing.T, result harness.ToolResult, message string) {
	t.Helper()
	want, err := json.Marshal(struct {
		Message string `json:"message"`
		Name    string `json:"name"`
		Code    string `json:"code"`
		Info    struct {
			Name string `json:"name"`
			Code string `json:"code"`
		} `json:"info"`
	}{message, "ToolArgsError", "INVALID_ARGS", struct {
		Name string `json:"name"`
		Code string `json:"code"`
	}{"ToolArgsError", "INVALID_ARGS"}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || result.ConcludesTurn || len(result.Value) != 0 || len(result.AdditionalContexts) != 0 || string(result.Meta) != string(want) || len(result.Content) != 1 || result.Content[0].Type != harness.BlockText || result.Content[0].Text != "Error: "+message {
		t.Fatalf("exact nested diagnostic=%+v meta=%s want=%s", result, result.Meta, want)
	}
}

func todoNestedAssertNativeRejection(t *testing.T, result harness.ToolResult) {
	t.Helper()
	if !result.IsError || result.ConcludesTurn || len(result.Value) != 0 || len(result.Meta) != 0 || len(result.Content) != 1 || result.Content[0].Type != harness.BlockText || !strings.HasPrefix(result.Content[0].Text, "invalid tool arguments for todo_write:") {
		t.Fatalf("native rejection was reclassified: %+v", result)
	}
}

func TestTodoNestedArgumentsFrozenPublicSourceMatrix(t *testing.T) {
	if len(todoNestedSourceCases) != 55 {
		t.Fatal("frozen source call count changed")
	}
	unique := map[string]bool{}
	for _, tc := range todoNestedSourceCases {
		unique[tc.raw] = true
	}
	if len(unique) != 51 {
		t.Fatalf("frozen source input count=%d", len(unique))
	}
	// Compare identical raw strings through both modes, including domain errors
	// and the explicitly retained public-source/native representation residuals.
	for _, parallel := range []bool{false, true} {
		legacy := map[int]harness.ToolResult{}
		for _, mode := range []harness.ToolArgumentErrorMode{harness.ArgumentErrorsLegacy, harness.ArgumentErrorsDSHAlpha3Todo} {
			t.Run(fmt.Sprintf("parallel_%v_mode_%d", parallel, mode), func(t *testing.T) {
				h, agent, counters := todoNestedFixture(t, mode, ContentNormalizationDSHAlpha3, parallel)
				for index, tc := range todoNestedSourceCases {
					if tc.parallel != parallel {
						continue
					}
					t.Run(fmt.Sprintf("%02d_%s", index+1, tc.id), func(t *testing.T) {
						before := *counters
						snapshots := todoSnapshots(agent.Session())
						current, found, err := Current(agent.Session())
						if err != nil {
							t.Fatal(err)
						}
						out, err := h.Tools().Execute(t.Context(), harness.ToolRequest{Agent: agent, Name: ToolName, Arguments: json.RawMessage(tc.raw)})
						if err != nil {
							t.Fatal(err)
						}
						schemaRejected := tc.category == "schema" || tc.category == "representation-residual-schema" || tc.id == "residual-negative-zero-preempts-schema"
						wantCalls := todoNestedCounters{before: 1}
						if !schemaRejected {
							wantCalls = todoNestedCounters{1, 1, 1, 1, 1, 1}
						}
						if got := counters.minus(before); got != wantCalls {
							t.Fatalf("admission changed: %+v want=%+v", got, wantCalls)
						}
						success := tc.category == "success" || tc.category == "success-parallel-policy" || tc.id == "residual-negative-zero-root-extra"
						if out.Result.IsError == success {
							t.Fatalf("result admission=%+v success=%v", out.Result, success)
						}
						if schemaRejected {
							if mode == harness.ArgumentErrorsDSHAlpha3Todo && tc.category == "schema" {
								todoNestedAssertDiagnostic(t, out.Result, tc.message)
							} else {
								todoNestedAssertNativeRejection(t, out.Result)
							}
						} else if tc.category == "domain" {
							wantMeta, _ := json.Marshal(struct {
								Message string `json:"message"`
							}{tc.message})
							if string(out.Result.Meta) != string(wantMeta) || len(out.Result.Value) != 0 || len(out.Result.Content) != 1 || out.Result.Content[0].Text != tc.content {
								t.Fatalf("plain body-domain error changed: %+v", out.Result)
							}
						} else if tc.category == "representation-residual-success" {
							// Native lossless decoding rejects a lone surrogate inside the
							// admitted Todo body; it must not become a schema ToolArgsError.
							if strings.Contains(string(out.Result.Meta), "ToolArgsError") || strings.Contains(string(out.Result.Meta), "INVALID_ARGS") || len(out.Result.Value) != 0 {
								t.Fatalf("body decoding was reclassified: %+v", out.Result)
							}
						}
						if success {
							wantValue, wantText := tc.value, tc.content
							if tc.id == "residual-negative-zero-root-extra" {
								// The mapper is passive: native validation accepts open -0.
								wantValue = `{"todos":[],"counts":{"pending":0,"inProgress":0,"completed":0}}`
								wantText = "Updated todo list: 0 pending, 0 in progress, 0 completed."
							}
							var gotValue, expected any
							if json.Unmarshal(out.Result.Value, &gotValue) != nil || json.Unmarshal([]byte(wantValue), &expected) != nil || !reflect.DeepEqual(gotValue, expected) || len(out.Result.Content) != 1 || out.Result.Content[0].Text != wantText {
								t.Fatalf("source success changed: %+v want=%s", out.Result, wantValue)
							}
							var value todoToolValue
							if err := json.Unmarshal(out.Result.Value, &value); err != nil {
								t.Fatal(err)
							}
							after := todoSnapshots(agent.Session())
							wantSnapshot, _ := json.Marshal(normalizedWriteEvent{Todos: value.Todos, Normalization: ecmascriptTrimV1})
							if len(after) != len(snapshots)+1 || string(after[len(after)-1]) != string(wantSnapshot) {
								t.Fatalf("success did not commit exactly once: %q", after)
							}
							got, present, err := Current(agent.Session())
							if err != nil || !present || !reflect.DeepEqual(got, value.Todos) {
								t.Fatalf("success projection=%+v/%v %v", got, present, err)
							}
						} else {
							got, present, err := Current(agent.Session())
							if err != nil || present != found || !reflect.DeepEqual(got, current) || !reflect.DeepEqual(todoSnapshots(agent.Session()), snapshots) {
								t.Fatalf("rejection changed Todo state: %+v/%v %v", got, present, err)
							}
						}
						if mode == harness.ArgumentErrorsLegacy {
							legacy[index] = out.Result
						} else if !schemaRejected {
							// Each harness owns a distinct private presentation receipt.
							// Compare every public result field, including exact canonical
							// bytes and nil/empty distinctions, without comparing provenance.
							previous := legacy[index]
							if !reflect.DeepEqual(out.Result.Value, previous.Value) || !reflect.DeepEqual(out.Result.Content, previous.Content) || out.Result.IsError != previous.IsError || !reflect.DeepEqual(out.Result.Meta, previous.Meta) || !reflect.DeepEqual(out.Result.AdditionalContexts, previous.AdditionalContexts) || out.Result.ConcludesTurn != previous.ConcludesTurn {
								t.Fatalf("diagnostic mode changed public admitted body result: %+v legacy=%+v", out.Result, previous)
							}
						}
					})
				}
			})
		}
	}
}

func TestTodoArgumentModeSelectionIsTrustedAndIndependent(t *testing.T) {
	for _, mode := range []harness.ToolArgumentErrorMode{harness.ArgumentErrorsLegacy, harness.ArgumentErrorsDSHAlpha3Todo} {
		for _, normalization := range []ContentNormalizationMode{ContentNormalizationLegacy, ContentNormalizationDSHAlpha3} {
			t.Run(fmt.Sprintf("mode_%d_normalization_%d", mode, normalization), func(t *testing.T) {
				h, agent, _ := todoNestedFixture(t, mode, normalization, false)
				out, err := h.Tools().Execute(t.Context(), harness.ToolRequest{Agent: agent, Name: ToolName, Arguments: json.RawMessage(`{"ArgumentErrorMode":2,"meta":{"ArgumentErrorMode":2},"reply":{"ArgumentErrorMode":2}}`)})
				if err != nil {
					t.Fatal(err)
				}
				if mode == harness.ArgumentErrorsLegacy {
					todoNestedAssertNativeRejection(t, out.Result)
				} else {
					todoNestedAssertDiagnostic(t, out.Result, `invalid arguments: missing required property "todos"`)
				}
				out, err = h.Tools().Execute(t.Context(), harness.ToolRequest{Agent: agent, Name: ToolName, Arguments: json.RawMessage(`{"todos":[{"content":"\ufeff keep \ufeff","status":"pending"}]}`)})
				if err != nil || out.Result.IsError {
					t.Fatalf("normalization control=%+v %v", out.Result, err)
				}
				want := "\ufeff keep \ufeff"
				if normalization == ContentNormalizationDSHAlpha3 {
					want = "keep"
				}
				var value todoToolValue
				if json.Unmarshal(out.Result.Value, &value) != nil || len(value.Todos) != 1 || value.Todos[0].Content != want {
					t.Fatalf("diagnostics selected normalization: %s want=%q", out.Result.Value, want)
				}
				var event map[string]json.RawMessage
				snapshots := todoSnapshots(agent.Session())
				if len(snapshots) != 1 || json.Unmarshal(snapshots[0], &event) != nil {
					t.Fatal("missing single write")
				}
				_, tagged := event["normalization"]
				if tagged != (normalization == ContentNormalizationDSHAlpha3) {
					t.Fatal("argument mode selected durable normalization")
				}
			})
		}
	}
	for _, normalization := range []ContentNormalizationMode{ContentNormalizationLegacy, ContentNormalizationDSHAlpha3} {
		for _, mode := range []harness.ToolArgumentErrorMode{harness.ArgumentErrorsDSHAlpha3Flat, harness.ToolArgumentErrorMode(255)} {
			for _, open := range []bool{false, true} {
				t.Run(fmt.Sprintf("invalid_%d_normalization_%d_open_%v", mode, normalization, open), func(t *testing.T) {
					h, _ := todoDomainHarness(t)
					wrappers := 0
					_, err := Mount(t.Context(), h, Options{ArgumentErrorMode: mode, ContentNormalization: normalization, AllowAdditionalInputProperties: open, ToolWrapper: func(tool harness.Tool) harness.Tool { wrappers++; return tool }})
					if err == nil || err.Error() != "todos: unsupported argument error mode" || wrappers != 0 {
						t.Fatalf("invalid mode accepted: %v wrappers=%d", err, wrappers)
					}
					if _, ok := h.Tools().Resolve(h.Runtime().Root(), ToolName); ok {
						t.Fatal("invalid mode registered a tool")
					}
				})
			}
		}
	}
	t.Run("closed root rejects explicit new mode", func(t *testing.T) {
		h, _ := todoDomainHarness(t)
		if _, err := Mount(t.Context(), h, Options{ArgumentErrorMode: harness.ArgumentErrorsDSHAlpha3Todo}); err == nil {
			t.Fatal("diagnostics silently opened ordinary Todo schema")
		}
		if _, ok := h.Tools().Resolve(h.Runtime().Root(), ToolName); ok {
			t.Fatal("failed descriptor left a registered tool")
		}
	})
	var options Options
	if err := json.Unmarshal([]byte(`{"ArgumentErrorMode":2,"argumentErrorMode":2,"ContentNormalization":1,"AllowAdditionalInputProperties":true}`), &options); err != nil {
		t.Fatal(err)
	}
	if options.ArgumentErrorMode != harness.ArgumentErrorsLegacy || options.ContentNormalization != ContentNormalizationLegacy || options.AllowAdditionalInputProperties {
		t.Fatalf("JSON selected trusted options: %+v", options)
	}
	// Options also carries a non-serializable function; inspect the actual field
	// tag rather than replacing the production type with a serializable stand-in.
	field, ok := reflect.TypeOf(Options{}).FieldByName("ArgumentErrorMode")
	if !ok || field.Tag.Get("json") != "-" {
		t.Fatalf("trusted mode lacks JSON exclusion: %+v", field)
	}
}

func TestTodoDuplicateMembersUseCapturedSemanticsAfterAdmission(t *testing.T) {
	cases := []struct {
		name, raw, kind      string
		pointSuccess         bool
		value, text, message string
	}{
		{"content-number-then-valid", "{\"todos\":[{\"content\":3,\"content\":\" kept \",\"status\":\"pending\"}]}", "success", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", ""},
		{"content-boolean-then-valid", "{\"todos\":[{\"content\":false,\"content\":\" kept \",\"status\":\"pending\"}]}", "success", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", ""},
		{"content-array-then-valid", "{\"todos\":[{\"content\":[],\"content\":\" kept \",\"status\":\"pending\"}]}", "success", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", ""},
		{"content-object-then-valid", "{\"todos\":[{\"content\":{},\"content\":\" kept \",\"status\":\"pending\"}]}", "success", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", ""},
		{"content-null-then-valid", "{\"todos\":[{\"content\":null,\"content\":\" kept \",\"status\":\"pending\"}]}", "success", true, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", ""},
		{"status-number-then-valid", "{\"todos\":[{\"content\":\" kept \",\"status\":3,\"status\":\"pending\"}]}", "success", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", ""},
		{"status-boolean-then-valid", "{\"todos\":[{\"content\":\" kept \",\"status\":false,\"status\":\"pending\"}]}", "success", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", ""},
		{"status-array-then-valid", "{\"todos\":[{\"content\":\" kept \",\"status\":[],\"status\":\"pending\"}]}", "success", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", ""},
		{"status-object-then-valid", "{\"todos\":[{\"content\":\" kept \",\"status\":{},\"status\":\"pending\"}]}", "success", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", ""},
		{"status-null-then-valid", "{\"todos\":[{\"content\":\" kept \",\"status\":null,\"status\":\"pending\"}]}", "success", true, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", ""},
		{"both-wrong-then-valid", "{\"todos\":[{\"content\":[],\"status\":{},\"content\":\"kept\",\"status\":\"pending\"}]}", "success", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", ""},
		{"two-valid-last-wins", "{\"todos\":[{\"content\":\"old\",\"content\":\" kept \",\"status\":\"completed\",\"status\":\"pending\"}]}", "success", true, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", ""},
		{"root-wrong-then-valid", "{\"todos\":false,\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}]}", "success", true, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", ""},
		{"escaped-known-members", "{\"todos\":[{\"content\":3,\"con\\u0074ent\":\"kept\",\"status\":false,\"stat\\u0075s\":\"pending\"}]}", "success", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", ""},
		{"escaped-root-last-array", "{\"todos\":false,\"to\\u0064os\":[{\"content\":[],\"con\\u0074ent\":\"kept\",\"status\":\"pending\"}]}", "success", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", ""},
		{"multiple-items", "{\"todos\":[{\"content\":3,\"content\":\"first\",\"status\":\"pending\"},{\"content\":\"second\",\"status\":[],\"status\":\"completed\"}]}", "success", false, "{\"todos\":[{\"content\":\"first\",\"status\":\"pending\"},{\"content\":\"second\",\"status\":\"completed\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":1}}", "Updated todo list: 1 pending, 0 in progress, 1 completed.", ""},
		{"forged-semantic-option", "{\"ContentNormalization\":1,\"ArgumentErrorMode\":2,\"meta\":{\"normalization\":\"ecmascript-trim-v1\"},\"todos\":[{\"content\":3,\"content\":\"kept\",\"status\":\"pending\"}]}", "success", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", ""},
		{"content-final-number", "{\"todos\":[{\"content\":\"kept\",\"content\":3,\"status\":\"pending\"}]}", "schema", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", "invalid arguments: \"todos[0].content\" must be a string"},
		{"status-final-number", "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\",\"status\":3}]}", "schema", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", "invalid arguments: \"todos[0].status\" must be a string"},
		{"content-final-null", "{\"todos\":[{\"content\":\"kept\",\"content\":null,\"status\":\"pending\"}]}", "schema", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", "invalid arguments: \"todos[0].content\" must be a string"},
		{"status-final-null", "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\",\"status\":null}]}", "schema", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", "invalid arguments: \"todos[0].status\" must be a string"},
		{"status-final-enum", "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\",\"status\":\"doing\"}]}", "schema", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", "invalid arguments: \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]"},
		{"root-final-invalid", "{\"todos\":[],\"todos\":false}", "schema", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", "invalid arguments: \"todos\" must be an array"},
		{"case-alias-closed", "{\"todos\":[{\"CONTENT\":3,\"content\":\"kept\",\"status\":\"pending\"}]}", "schema", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", "invalid arguments: \"todos[0].CONTENT\" is not a declared property (additionalProperties: false)"},
		{"case-alias-required", "{\"todos\":[{\"Content\":\"kept\",\"status\":\"pending\"}]}", "schema", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", "invalid arguments: missing required property \"todos[0].content\"; \"todos[0].Content\" is not a declared property (additionalProperties: false)"},
		{"duplicate-unknown-closed", "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\",\"extra\":1,\"extra\":2}]}", "schema", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", "invalid arguments: \"todos[0].extra\" is not a declared property (additionalProperties: false)"},
		{"final-list-missing-content", "{\"todos\":[{\"content\":\"old\",\"status\":\"pending\"}],\"todos\":[{\"status\":\"pending\"}]}", "schema", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", "invalid arguments: missing required property \"todos[0].content\""},
		{"last-value-domain-blank", "{\"todos\":[{\"content\":3,\"content\":\" \",\"status\":\"pending\"}]}", "domain", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", "invalid todo: `content` must be a non-empty string"},
		{"overwritten-content-surrogate", "{\"todos\":[{\"content\":\"\\ud800\",\"content\":\"kept\",\"status\":\"pending\"}]}", "representation-residual-success", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", ""},
		{"overwritten-status-surrogate", "{\"todos\":[{\"content\":\"kept\",\"status\":\"\\ud800\",\"status\":\"pending\"}]}", "representation-residual-success", false, "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "Updated todo list: 1 pending, 0 in progress, 0 completed.", ""},
	}
	// Native-only malformed UTF-8 is not a source JSON.parse parity input.
	cases = append(cases, struct {
		name, raw, kind      string
		pointSuccess         bool
		value, text, message string
	}{name: "overwritten-invalid-utf8", raw: "{\"todos\":[{\"content\":\"\xff\",\"content\":\"kept\",\"status\":\"pending\"}]}", kind: "invalid-utf8"})
	for _, normalization := range []ContentNormalizationMode{ContentNormalizationLegacy, ContentNormalizationDSHAlpha3} {
		for _, mode := range []harness.ToolArgumentErrorMode{harness.ArgumentErrorsLegacy, harness.ArgumentErrorsDSHAlpha3Todo} {
			t.Run(fmt.Sprintf("normalization_%d_diagnostics_%d", normalization, mode), func(t *testing.T) {
				h, agent, counters := todoNestedFixture(t, mode, normalization, false)
				for _, tc := range cases {
					t.Run(tc.name, func(t *testing.T) {
						beforeCalls := *counters
						beforeWrites := todoSnapshots(agent.Session())
						previous, found, err := Current(agent.Session())
						if err != nil {
							t.Fatal(err)
						}
						out, err := h.Tools().Execute(t.Context(), harness.ToolRequest{Agent: agent, Name: ToolName, Arguments: json.RawMessage(tc.raw)})
						if err != nil || string(out.Request.Arguments) != tc.raw {
							t.Fatalf("raw request/audit changed: %q %v", out.Request.Arguments, err)
						}
						calls := todoNestedCounters{1, 1, 1, 1, 1, 1}
						if tc.kind == "schema" {
							calls = todoNestedCounters{before: 1}
						}
						if got := counters.minus(beforeCalls); got != calls {
							t.Fatalf("admission order changed: %+v want=%+v", got, calls)
						}
						success := tc.kind == "success" && (normalization == ContentNormalizationDSHAlpha3 || tc.pointSuccess)
						if out.Result.IsError == success || out.Result.ConcludesTurn || len(out.Result.AdditionalContexts) != 0 {
							t.Fatalf("duplicate result=%+v success=%v", out.Result, success)
						}
						if success {
							if string(out.Result.Value) != tc.value || len(out.Result.Meta) != 0 || len(out.Result.Content) != 1 || out.Result.Content[0].Type != harness.BlockText || out.Result.Content[0].Text != tc.text {
								t.Fatalf("last-member result drift: %+v", out.Result)
							}
							var value todoToolValue
							if err := json.Unmarshal(out.Result.Value, &value); err != nil {
								t.Fatal(err)
							}
							var event any = writeEvent{Todos: value.Todos}
							if normalization == ContentNormalizationDSHAlpha3 {
								event = normalizedWriteEvent{Todos: value.Todos, Normalization: ecmascriptTrimV1}
							}
							want, _ := json.Marshal(event)
							writes := todoSnapshots(agent.Session())
							if len(writes) != len(beforeWrites)+1 || string(writes[len(writes)-1]) != string(want) {
								t.Fatalf("accepted duplicate must append exactly once: %q", writes)
							}
							current, present, err := Current(agent.Session())
							if err != nil || !present || !reflect.DeepEqual(current, value.Todos) {
								t.Fatalf("last-value replay drift: %+v %v", current, err)
							}
						} else {
							current, present, err := Current(agent.Session())
							if err != nil || present != found || !reflect.DeepEqual(current, previous) || !reflect.DeepEqual(todoSnapshots(agent.Session()), beforeWrites) || len(out.Result.Value) != 0 {
								t.Fatalf("rejected duplicate changed durable state: %+v %v", out.Result, err)
							}
							if tc.kind == "schema" {
								if mode == harness.ArgumentErrorsDSHAlpha3Todo {
									todoNestedAssertDiagnostic(t, out.Result, tc.message)
								} else {
									todoNestedAssertNativeRejection(t, out.Result)
								}
							} else if strings.Contains(string(out.Result.Meta), "ToolArgsError") || strings.Contains(string(out.Result.Meta), "INVALID_ARGS") {
								t.Fatalf("body failure became schema failure: %+v", out.Result)
							} else if tc.kind == "representation-residual-success" {
								if len(out.Result.Content) != 1 || !strings.Contains(out.Result.Content[0].Text, "lone escaped UTF-16 surrogate") {
									t.Fatalf("duplicate collapse weakened raw Unicode guard: %+v", out.Result)
								}
							} else if tc.kind == "invalid-utf8" {
								if len(out.Result.Content) != 1 || !strings.Contains(out.Result.Content[0].Text, "JSON string cannot round-trip without changing its value") {
									t.Fatalf("duplicate collapse weakened raw UTF-8 guard: %+v", out.Result)
								}
							} else if normalization == ContentNormalizationDSHAlpha3 {
								want, _ := json.Marshal(map[string]string{"message": tc.message})
								if string(out.Result.Meta) != string(want) || len(out.Result.Content) != 1 || out.Result.Content[0].Text != "Error: "+tc.message {
									t.Fatalf("last-value domain error drift: %+v", out.Result)
								}
							} else if len(out.Result.Content) != 1 || !strings.Contains(out.Result.Content[0].Text, "cannot unmarshal") {
								t.Fatalf("ordinary Point DTO changed: %+v", out.Result)
							}
						}
					})
				}
			})
		}
	}
}

func TestTodoDuplicateDecodingPreservesClosedRootAndAcceptedSize(t *testing.T) {
	for _, normalization := range []ContentNormalizationMode{ContentNormalizationLegacy, ContentNormalizationDSHAlpha3} {
		t.Run(fmt.Sprintf("closed_root_%d", normalization), func(t *testing.T) {
			h, _ := todoDomainHarness(t)
			bodies := 0
			if _, err := Mount(t.Context(), h, Options{ContentNormalization: normalization, ToolWrapper: func(tool harness.Tool) harness.Tool {
				original := tool.ExecuteRequest
				tool.ExecuteRequest = func(ctx context.Context, request harness.ToolRequest) (harness.ToolResult, error) {
					bodies++
					return original(ctx, request)
				}
				return tool
			}}); err != nil {
				t.Fatal(err)
			}
			agent, err := h.Agents().Create(t.Context(), harness.AgentOptions{ID: "todo-closed-duplicates"})
			if err != nil {
				t.Fatal(err)
			}
			for _, raw := range []string{`{"todos":[{"content":3,"content":"kept","status":"pending"}],"extra":1}`, `{"todos":[{"Content":"kept","status":"pending"}]}`} {
				out, err := h.Tools().Execute(t.Context(), harness.ToolRequest{Agent: agent, Name: ToolName, Arguments: json.RawMessage(raw)})
				if err != nil || !out.Result.IsError || bodies != 0 || len(todoSnapshots(agent.Session())) != 0 {
					t.Fatalf("closed validation changed: %+v %v bodies=%d", out.Result, err, bodies)
				}
			}
			out, err := h.Tools().Execute(t.Context(), harness.ToolRequest{Agent: agent, Name: ToolName, Arguments: json.RawMessage(`{"todos":[{"content":3,"content":"kept","status":"pending"}]}`)})
			want := 0
			if normalization == ContentNormalizationDSHAlpha3 {
				want = 1
			}
			if err != nil || out.Result.IsError == (want == 1) || bodies != 1 || len(todoSnapshots(agent.Session())) != want {
				t.Fatalf("closed-root semantics changed: %+v %v", out.Result, err)
			}
		})
	}
	for _, mode := range []harness.ToolArgumentErrorMode{harness.ArgumentErrorsLegacy, harness.ArgumentErrorsDSHAlpha3Todo} {
		t.Run(fmt.Sprintf("large_admitted_diagnostics_%d", mode), func(t *testing.T) {
			h, agent, counters := todoNestedFixture(t, mode, ContentNormalizationDSHAlpha3, false)
			// The passive diagnostic byte cap must not become an accepted-call cap.
			content := strings.Repeat("x", (1<<20)+1)
			raw := `{"todos":[{"content":3,"content":"` + content + `","status":"pending"}]}`
			out, err := h.Tools().Execute(t.Context(), harness.ToolRequest{Agent: agent, Name: ToolName, Arguments: json.RawMessage(raw)})
			if err != nil || out.Result.IsError || *counters != (todoNestedCounters{1, 1, 1, 1, 1, 1}) || len(todoSnapshots(agent.Session())) != 1 || string(out.Request.Arguments) != raw {
				t.Fatalf("diagnostic budget changed valid admission: %v error=%v calls=%+v", err, out.Result.IsError, *counters)
			}
			current, found, err := Current(agent.Session())
			if err != nil || !found || len(current) != 1 || current[0].Content != content {
				t.Fatal("large duplicate final value changed")
			}
		})
	}
}

func TestTodoNestedArgumentCapturedDispatchUsesAdmittedOuterAndExactNativeOwner(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	h, _ := todoDomainHarness(t)
	nativeBodies, replacementBodies := 0, 0
	owner, err := Mount(ctx, h, Options{ContentNormalization: ContentNormalizationDSHAlpha3, ArgumentErrorMode: harness.ArgumentErrorsDSHAlpha3Todo, AllowAdditionalInputProperties: true, ToolWrapper: func(tool harness.Tool) harness.Tool {
		original := tool.ExecuteRequest
		tool.ExecuteRequest = func(ctx context.Context, request harness.ToolRequest) (harness.ToolResult, error) {
			nativeBodies++
			return original(ctx, request)
		}
		return tool
	}})
	if err != nil {
		t.Fatal(err)
	}
	var base harness.ToolCatalogBase
	var captured harness.Tool
	var savedCtx context.Context
	var savedRequest harness.ToolRequest
	var dispatchErr error
	outer := todoNestedCounters{}
	proxySchema := harness.ToolSchema{Name: ToolName, InputSchema: json.RawMessage(`{"type":"object"}`)}
	// A deliberately broader, trusted test presenter reaches the captured
	// native validation branch using a genuine outer admission. It does not
	// bind or claim the Todo argument projection on its different schema.
	proxy := harness.Tool{Schema: proxySchema, RequiresApproval: true, ApprovalReason: func(harness.ToolRequest) string { outer.reason++; return "synthetic captured Todo" }, ExecuteRequest: func(callCtx context.Context, request harness.ToolRequest) (harness.ToolResult, error) {
		outer.body++
		if savedCtx != nil {
			// This is an authentic prior request with an expired, different token;
			// no admission context or private execution token is fabricated.
			if _, err := base.DispatchCaptured(callCtx, savedRequest, captured); err == nil {
				t.Error("prior request token accepted in a new admitted body")
			}
		}
		forged := harness.Tool{Schema: captured.Schema, ArgumentErrorMode: harness.ArgumentErrorsDSHAlpha3Todo}
		if _, err := base.DispatchCaptured(callCtx, request, forged); err == nil {
			t.Error("public fields forged an exact native capture")
		}
		savedCtx, savedRequest = callCtx, request
		result, err := base.DispatchCaptured(callCtx, request, captured)
		dispatchErr = err
		return result, err
	}}
	agent, err := h.Agents().Create(ctx, harness.AgentOptions{ID: "todo-captured-branch", Configure: func(_ context.Context, run *point.Run, _ *harness.Agent) error {
		if _, err := h.Tools().RegisterCatalogProvider(run, "todo-captured-presenter", harness.ToolCatalogProvider{Authoritative: true, Bind: func(view harness.ToolCatalogBase) { base = view }, Schemas: func(context.Context, *harness.Agent) ([]harness.ToolSchema, error) {
			return []harness.ToolSchema{proxySchema}, nil
		}, Resolve: func(_ context.Context, _ *harness.Agent, name string) (harness.Tool, bool, error) {
			return proxy, name == ToolName, nil
		}}); err != nil {
			return err
		}
		if _, err := h.Tools().Before.Use(run, func(_ context.Context, request harness.ToolRequest, next point.Next[harness.ToolRequest]) (harness.ToolRequest, error) {
			outer.before++
			return next(request)
		}); err != nil {
			return err
		}
		if _, err := h.Tools().After.Use(run, func(_ context.Context, out harness.ToolOutcome, next point.Next[harness.ToolOutcome]) (harness.ToolOutcome, error) {
			outer.after++
			return next(out)
		}); err != nil {
			return err
		}
		if _, err := h.Tools().Guard(run, func(context.Context, harness.ToolRequest) error { outer.guard++; return nil }); err != nil {
			return err
		}
		_, err := h.Approvals().Register(run, func(context.Context, harness.ApprovalRequest) (harness.ApprovalOutcome, error) {
			outer.approval++
			return harness.ApprovalAllowedOnce, nil
		})
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Session().Append("turn/start", map[string]int{"turn": 1}); err != nil {
		t.Fatal(err)
	}
	var found bool
	captured, found, err = base.Resolve(ctx, agent, ToolName)
	if err != nil || !found {
		t.Fatalf("native capture=%v %v", found, err)
	}
	invalid := json.RawMessage(`{"todos":[{"status":"doing","content":null,"zzz":1}]}`)
	if _, err := base.DispatchCaptured(ctx, harness.ToolRequest{Agent: agent, Name: ToolName, Arguments: invalid}, captured); err == nil {
		t.Fatal("outside-admission dispatch accepted")
	}
	execute := func(raw json.RawMessage) harness.ToolResult {
		t.Helper()
		before := outer
		out, err := h.Tools().Execute(ctx, harness.ToolRequest{Agent: agent, Name: ToolName, Arguments: raw})
		if err != nil {
			t.Fatal(err)
		}
		if got := outer.minus(before); got != (todoNestedCounters{1, 1, 1, 1, 1, 1}) {
			t.Fatalf("outer admission counters=%+v", got)
		}
		if _, err := base.DispatchCaptured(savedCtx, savedRequest, captured); err == nil {
			t.Fatal("captured body authority survived outer return")
		}
		return out.Result
	}
	result := execute(invalid)
	todoNestedAssertDiagnostic(t, result, `invalid arguments: "todos[0].content" must be a string; "todos[0].status" must be one of ["pending","in_progress","completed"]; "todos[0].zzz" is not a declared property (additionalProperties: false)`)
	if dispatchErr != nil || nativeBodies != 0 || len(todoSnapshots(agent.Session())) != 0 {
		t.Fatalf("captured rejection reached native body: %v bodies=%d", dispatchErr, nativeBodies)
	}
	// Public capture edits cannot alter its private native schema or executor.
	captured.ArgumentErrorMode = harness.ArgumentErrorsLegacy
	captured.Schema.InputSchema = json.RawMessage(`{"type":"object"}`)
	captured.ExecuteRequest = func(context.Context, harness.ToolRequest) (harness.ToolResult, error) {
		replacementBodies++
		return harness.ToolResult{}, nil
	}
	result = execute(invalid)
	todoNestedAssertDiagnostic(t, result, `invalid arguments: "todos[0].content" must be a string; "todos[0].status" must be one of ["pending","in_progress","completed"]; "todos[0].zzz" is not a declared property (additionalProperties: false)`)
	if dispatchErr != nil || nativeBodies != 0 || replacementBodies != 0 || len(todoSnapshots(agent.Session())) != 0 {
		t.Fatal("public capture edits changed native rejection")
	}
	result = execute(json.RawMessage(`{"todos":[]}`))
	if result.IsError || dispatchErr != nil || nativeBodies != 1 || replacementBodies != 0 || len(todoSnapshots(agent.Session())) != 1 {
		t.Fatalf("valid captured control=%+v %v native=%d replacement=%d", result, dispatchErr, nativeBodies, replacementBodies)
	}
	result = execute(json.RawMessage(`{"todos":[{"content":3,"content":"kept","status":false,"status":"pending"}]}`))
	if result.IsError || dispatchErr != nil || nativeBodies != 2 || replacementBodies != 0 || len(todoSnapshots(agent.Session())) != 2 || string(result.Value) != `{"todos":[{"content":"kept","status":"pending"}],"counts":{"pending":1,"inProgress":0,"completed":0}}` {
		t.Fatalf("captured DSH last-member decoding=%+v %v", result, dispatchErr)
	}
	if err := owner.Dispose(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := Mount(ctx, h, Options{AllowAdditionalInputProperties: true, ToolWrapper: func(tool harness.Tool) harness.Tool {
		original := tool.ExecuteRequest
		tool.ExecuteRequest = func(ctx context.Context, request harness.ToolRequest) (harness.ToolResult, error) {
			replacementBodies++
			return original(ctx, request)
		}
		return tool
	}}); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []json.RawMessage{invalid, json.RawMessage(`{"todos":[]}`)} {
		result = execute(raw)
		if !result.IsError || nativeBodies != 2 || replacementBodies != 0 || len(todoSnapshots(agent.Session())) != 2 || strings.Contains(string(result.Meta), "ToolArgsError") {
			t.Fatalf("stale native capture reached replacement/projector: %+v native=%d replacement=%d", result, nativeBodies, replacementBodies)
		}
	}
}

// Stage40: each raw overflow control is independently exercised by the public source oracle.
var todoNumberCases = []struct{ id, raw, kind, value, message string }{
	{"overflow-content-positive", "{\"todos\":[{\"content\":1e309,\"content\":\"kept\",\"status\":\"pending\"}]}", "success", "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", ""},
	{"overflow-content-negative", "{\"todos\":[{\"content\":-1e309,\"content\":\"kept\",\"status\":\"pending\"}]}", "success", "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", ""},
	{"overflow-status-positive", "{\"todos\":[{\"content\":\"kept\",\"status\":1e309,\"status\":\"pending\"}]}", "success", "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", ""},
	{"overflow-status-negative", "{\"todos\":[{\"content\":\"kept\",\"status\":-1e309,\"status\":\"pending\"}]}", "success", "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", ""},
	{"overflow-escaped-keys", "{\"todos\":[{\"content\":1e309,\"con\\u0074ent\":\"kept\",\"status\":-1e309,\"stat\\u0075s\":\"pending\"}]}", "success", "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", ""},
	{"overflow-overwritten-subtree", "{\"todos\":[{\"content\":{\"n\":[1e309]},\"content\":\"kept\",\"status\":\"pending\"}]}", "success", "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", ""},
	{"overflow-root-last-array", "{\"todos\":1e309,\"to\\u0064os\":[]}", "success", "{\"todos\":[],\"counts\":{\"pending\":0,\"inProgress\":0,\"completed\":0}}", ""},
	{"overflow-extra-last-value", "{\"ignored\":1e309,\"ignored\":0,\"todos\":[]}", "success", "{\"todos\":[],\"counts\":{\"pending\":0,\"inProgress\":0,\"completed\":0}}", ""},
	{"overflow-extreme-exponent", "{\"todos\":[{\"content\":1e999999,\"content\":\"kept\",\"status\":\"pending\"}]}", "success", "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", ""},
	{"overflow-forged-mode-fields", "{\"dshTodoNumberCompatibility\":true,\"ContentNormalization\":1,\"meta\":{\"mode\":\"dsh\"},\"todos\":[{\"content\":1e309,\"content\":\"kept\",\"status\":\"pending\"}]}", "success", "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", ""},
	{"overflow-final-null", "{\"todos\":[{\"content\":1e309,\"content\":null,\"status\":\"pending\"}]}", "schema-residual", "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "invalid arguments: \"todos[0].content\" must be a string"},
	{"overflow-final-enum", "{\"todos\":[{\"content\":\"kept\",\"status\":1e309,\"status\":\"doing\"}]}", "schema-residual", "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "invalid arguments: \"todos[0].status\" must be one of [\"pending\",\"in_progress\",\"completed\"]"},
	{"overflow-unknown-member", "{\"todos\":[{\"content\":1e309,\"content\":\"kept\",\"status\":\"pending\",\"extra\":1}]}", "schema-residual", "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "invalid arguments: \"todos[0].extra\" is not a declared property (additionalProperties: false)"},
	{"overflow-survives-known", "{\"todos\":[{\"content\":\"kept\",\"content\":1e309,\"status\":\"pending\"}]}", "lossless-residual", "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "tool execution arguments must be losslessly JSON-serializable"},
	{"overflow-survives-extra", "{\"todos\":[],\"extra\":1e309}", "lossless-residual", "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "tool execution arguments must be losslessly JSON-serializable"},
	{"overflow-overwritten-surrogate", "{\"todos\":[{\"content\":\"\\ud800\",\"content\":1e309,\"content\":\"kept\",\"status\":\"pending\"}]}", "representation-residual-success", "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", ""},
	{"overflow-final-blank", "{\"todos\":[{\"content\":1e309,\"content\":\" \",\"status\":\"pending\"}]}", "domain", "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", "invalid todo: `content` must be a non-empty string"},
	{"overflow-plain-control", "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}]}", "success", "{\"todos\":[{\"content\":\"kept\",\"status\":\"pending\"}],\"counts\":{\"pending\":1,\"inProgress\":0,\"completed\":0}}", ""},
}

func TestTodoOverwrittenNumbersUseOnlyCapturedSemanticMode(t *testing.T) {
	for _, semantic := range []ContentNormalizationMode{ContentNormalizationLegacy, ContentNormalizationDSHAlpha3} {
		for _, diagnostics := range []harness.ToolArgumentErrorMode{harness.ArgumentErrorsLegacy, harness.ArgumentErrorsDSHAlpha3Todo} {
			t.Run(fmt.Sprintf("semantic=%d_diagnostics=%d", semantic, diagnostics), func(t *testing.T) {
				h, agent, counters := todoNestedFixture(t, diagnostics, semantic, false)
				for _, tc := range todoNumberCases {
					before := *counters
					writes := len(todoSnapshots(agent.Session()))
					previous, found, err := Current(agent.Session())
					if err != nil {
						t.Fatal(err)
					}
					out, err := h.Tools().Execute(t.Context(), harness.ToolRequest{Agent: agent, Name: ToolName, Arguments: json.RawMessage(tc.raw)})
					if err != nil || string(out.Request.Arguments) != tc.raw {
						t.Fatalf("%s raw audit/outcome: %+v %v", tc.id, out, err)
					}
					body := tc.id == "overflow-plain-control" || semantic == ContentNormalizationDSHAlpha3 && (tc.kind == "success" || tc.kind == "domain" || tc.kind == "representation-residual-success")
					success := body && tc.kind == "success"
					// Schema-gate rejection returns before settle/After, as in the
					// established frozen and duplicate matrices above.
					wantCounters := todoNestedCounters{before: 1}
					if body {
						wantCounters = todoNestedCounters{1, 1, 1, 1, 1, 1}
					}
					if got := counters.minus(before); got != wantCounters {
						t.Fatalf("%s admission effects=%+v want=%+v", tc.id, got, wantCounters)
					}
					if success {
						if out.Result.IsError || string(out.Result.Value) != tc.value || len(out.Result.Meta) != 0 || out.Result.ConcludesTurn || len(out.Result.AdditionalContexts) != 0 || len(out.Result.Content) != 1 || out.Result.Content[0].Type != harness.BlockText {
							t.Fatalf("%s canonical result=%+v", tc.id, out.Result)
						}
						wantText := "Updated todo list: 1 pending, 0 in progress, 0 completed."
						if strings.Contains(tc.value, `"todos":[]`) {
							wantText = "Updated todo list: 0 pending, 0 in progress, 0 completed."
						}
						if out.Result.Content[0].Text != wantText {
							t.Fatalf("%s content=%+v", tc.id, out.Result.Content)
						}
						if len(todoSnapshots(agent.Session())) != writes+1 {
							t.Fatal("accepted call did not append exactly once")
						}
						var value struct {
							Todos []Item `json:"todos"`
						}
						if err := json.Unmarshal(out.Result.Value, &value); err != nil {
							t.Fatal(err)
						}
						current, ok, err := Current(agent.Session())
						if err != nil || !ok || !reflect.DeepEqual(current, value.Todos) {
							t.Fatalf("replay mismatch: %+v %v", current, err)
						}
					} else {
						if len(todoSnapshots(agent.Session())) != writes {
							t.Fatalf("%s rejected call appended", tc.id)
						}
						current, ok, err := Current(agent.Session())
						if err != nil || ok != found || !reflect.DeepEqual(current, previous) {
							t.Fatalf("%s rejected call changed durable projection", tc.id)
						}
						if !body && tc.kind == "schema-residual" && semantic == ContentNormalizationDSHAlpha3 && diagnostics == harness.ArgumentErrorsDSHAlpha3Todo {
							todoNestedAssertDiagnostic(t, out.Result, tc.message)
						} else if !body {
							todoNestedAssertNativeRejection(t, out.Result)
						} else if tc.kind == "representation-residual-success" {
							if !out.Result.IsError || len(out.Result.Value) != 0 || len(out.Result.Content) != 1 || !strings.Contains(out.Result.Content[0].Text, "lone escaped UTF-16 surrogate") {
								t.Fatalf("raw Unicode evidence erased: %+v", out.Result)
							}
						} else {
							if !out.Result.IsError || len(out.Result.Value) != 0 || len(out.Result.Content) != 1 || out.Result.Content[0].Text != "Error: "+tc.message || strings.Contains(string(out.Result.Meta), "ToolArgsError") {
								t.Fatalf("domain error changed: %+v", out.Result)
							}
						}
					}
				}
			})
		}
	}
}

func TestTodoNumberRetryPreservesInvalidUTF8AndClosedRoot(t *testing.T) {
	h, agent, counters := todoNestedFixture(t, harness.ArgumentErrorsLegacy, ContentNormalizationDSHAlpha3, false)
	raw := `{"todos":[{"content":"` + string([]byte{0xff}) + `","content":1e309,"content":"kept","status":"pending"}]}`
	out, err := h.Tools().Execute(t.Context(), harness.ToolRequest{Agent: agent, Name: ToolName, Arguments: json.RawMessage(raw)})
	if err != nil || !out.Result.IsError || len(out.Result.Content) != 1 || !strings.Contains(out.Result.Content[0].Text, "JSON string cannot round-trip") || string(out.Request.Arguments) != raw || *counters != (todoNestedCounters{1, 1, 1, 1, 1, 1}) || len(todoSnapshots(agent.Session())) != 0 {
		t.Fatalf("invalid UTF8 repaired by retry: %+v %v counters=%+v", out, err, *counters)
	}
	closed, _ := todoDomainHarness(t)
	if _, err := Mount(t.Context(), closed, Options{ContentNormalization: ContentNormalizationDSHAlpha3}); err != nil {
		t.Fatal(err)
	}
	closedAgent, err := closed.Agents().Create(t.Context(), harness.AgentOptions{ID: "todo-number-closed"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{`{"todos":[{"content":1e309,"content":"kept","status":"pending"}]}`, true},
		{`{"todos":[{"content":1e309,"content":"kept","status":"pending"}],"extra":true}`, false},
		{`{"todos":[{"content":1e309,"content":"kept","status":"pending","extra":true}]}`, false},
	} {
		before := len(todoSnapshots(closedAgent.Session()))
		out, err := closed.Tools().Execute(t.Context(), harness.ToolRequest{Agent: closedAgent, Name: ToolName, Arguments: json.RawMessage(tc.raw)})
		want := before
		if tc.ok {
			want++
		}
		if err != nil || out.Result.IsError == tc.ok || len(todoSnapshots(closedAgent.Session())) != want {
			t.Fatalf("closed schema admission=%+v %v", out.Result, err)
		}
	}
}

func TestTodoNumberRetryBudgetRejectionHasNoApprovalOrWrite(t *testing.T) {
	h, agent, counters := todoNestedFixture(t, harness.ArgumentErrorsDSHAlpha3Todo, ContentNormalizationDSHAlpha3, false)
	const base = `{"todos":[{"content":1e309,"content":"kept","status":"pending"}]}`
	for _, raw := range []string{
		base + strings.Repeat(" ", 1<<20),
		strings.TrimSuffix(base, "}") + `,"extra":[` + strings.Repeat("0,", 65536) + `0]}`,
		strings.TrimSuffix(base, "}") + `,"extra":` + strings.Repeat("[", 64) + `0` + strings.Repeat("]", 64) + `}`,
	} {
		before := *counters
		out, err := h.Tools().Execute(t.Context(), harness.ToolRequest{Agent: agent, Name: ToolName, Arguments: json.RawMessage(raw)})
		if err != nil || string(out.Request.Arguments) != raw || counters.minus(before) != (todoNestedCounters{before: 1}) || len(todoSnapshots(agent.Session())) != 0 {
			t.Fatalf("budget refusal caused effects: %v %+v", err, counters.minus(before))
		}
		todoNestedAssertNativeRejection(t, out.Result)
	}
}

func TestTodoPassiveNumberDiagnosticsDoNotReachAdmissionOrWrites(t *testing.T) {
	h, agent, counters := todoNestedFixture(t, harness.ArgumentErrorsDSHAlpha3Todo, ContentNormalizationDSHAlpha3, false)
	seed := json.RawMessage(`{"todos":[{"content":"seed","status":"completed"}]}`)
	if out, err := h.Tools().Execute(t.Context(), harness.ToolRequest{Agent: agent, Name: ToolName, Arguments: seed}); err != nil || out.Result.IsError {
		t.Fatalf("seed=%+v %v", out, err)
	}
	for _, tc := range []struct{ name, raw, message string }{
		{`final-content-null`, `{"todos":[{"content":1e309,"content":null,"status":"pending"}]}`, `invalid arguments: "todos[0].content" must be a string`},
		{`final-status-enum`, `{"todos":[{"content":"kept","status":1e309,"status":"doing"}]}`, `invalid arguments: "todos[0].status" must be one of ["pending","in_progress","completed"]`},
		{`unknown-member`, `{"todos":[{"content":1e309,"content":"kept","status":"pending","extra":1}]}`, `invalid arguments: "todos[0].extra" is not a declared property (additionalProperties: false)`},
		{`root-array`, `[{"x":1e309,"x":0}]`, `invalid arguments: "arguments" must be an object`},
		{`final-todos-null`, `{"todos":1e309,"todos":null}`, `invalid arguments: "todos" must be an array`},
		{`final-item-null`, `{"todos":1e309,"todos":[null]}`, `invalid arguments: "todos[0]" must be an object`},
		{`ordered-errors`, `{"todos":[{"zzz":1e309,"aaa":2,"zzz":0,"10":1,"2":1,"content":null,"status":"doing"}]}`, `invalid arguments: "todos[0].content" must be a string; "todos[0].status" must be one of ["pending","in_progress","completed"]; "todos[0].2" is not a declared property (additionalProperties: false); "todos[0].10" is not a declared property (additionalProperties: false); "todos[0].zzz" is not a declared property (additionalProperties: false); "todos[0].aaa" is not a declared property (additionalProperties: false)`},
		{`missing-property-index-order`, `{"todos":1e309,"todos":[{"zzz":0,"content":null},{}]}`, `invalid arguments: missing required property "todos[0].status"; "todos[0].content" must be a string; "todos[0].zzz" is not a declared property (additionalProperties: false); missing required property "todos[1].content"; missing required property "todos[1].status"`},
		{`escaped-duplicate`, `{"todos":[{"content":1e309,"con\u0074ent":null,"status":"pending"}]}`, `invalid arguments: "todos[0].content" must be a string`},
		{`schema-before-domain`, `{"todos":1e309,"todos":[{"content":" ","status":"pending"},null]}`, `invalid arguments: "todos[1]" must be an object`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, writes := *counters, len(todoSnapshots(agent.Session()))
			previous, found, err := Current(agent.Session())
			if err != nil {
				t.Fatal(err)
			}
			out, err := h.Tools().Execute(t.Context(), harness.ToolRequest{Agent: agent, Name: ToolName, Arguments: json.RawMessage(tc.raw)})
			if err != nil || string(out.Request.Arguments) != tc.raw || counters.minus(before) != (todoNestedCounters{before: 1}) || len(todoSnapshots(agent.Session())) != writes {
				t.Fatalf("rejection effects/raw changed: %+v %v counters=%+v", out, err, counters.minus(before))
			}
			todoNestedAssertDiagnostic(t, out.Result, tc.message)
			current, ok, err := Current(agent.Session())
			if err != nil || ok != found || !reflect.DeepEqual(current, previous) {
				t.Fatalf("rejection changed durable projection: %+v %v", current, err)
			}
		})
	}
}

