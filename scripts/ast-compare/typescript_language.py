"""Load the local grammar extension with explicit provenance, or stock upstream."""
import ctypes
import json
from tree_sitter import Language
import tree_sitter_typescript
from build_typescript import ARCHIVE_SHA256, BUILD, VERSION, digest

# Keep the shared library alive for every Language/Parser using its static tables.
_library = None


def load_typescript():
    global _library
    library = BUILD / "typescript.so"
    if not library.exists():
        return Language(tree_sitter_typescript.language_typescript()), {"version": "0.23.2", "extension": False}
    receipt = json.loads((BUILD / "receipt.json").read_text())
    if (receipt["version"] != VERSION or receipt["archive_sha256"] != ARCHIVE_SHA256
            or receipt["library_sha256"] != digest(library.read_bytes())):
        raise ValueError("local TypeScript grammar receipt mismatch; rebuild using build_typescript.py")
    _library = ctypes.CDLL(str(library))
    language = _library.tree_sitter_typescript
    language.restype = ctypes.c_void_p
    capsule = ctypes.pythonapi.PyCapsule_New
    capsule.restype = ctypes.py_object
    capsule.argtypes = [ctypes.c_void_p, ctypes.c_char_p, ctypes.c_void_p]
    return Language(capsule(language(), b"tree_sitter.Language", None)), receipt
