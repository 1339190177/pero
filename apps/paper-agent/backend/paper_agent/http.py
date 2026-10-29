"""Authenticated Pero App transport; domain and literature work stay separate."""
from datetime import datetime, timezone
import hmac
from http.server import BaseHTTPRequestHandler
import json
import re
from urllib.parse import parse_qs, urlsplit

from .literature import LiteratureClient
from .model import Invalid, PaperSnapshot, ResearchRequest, canonical, mapping
from .reports import EvidenceReference

MAX_BODY = 1024 * 1024


def utc_now():
    return datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")


def handler(store, secret, literature=None, now=utc_now):
    literature = literature if literature is not None else LiteratureClient()

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_args):
            pass

        def reply(self, status, value):
            body = canonical(value)
            self.send_response(status)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Cache-Control", "no-store")
            self.send_header("Content-Length", str(len(body)))
            self.send_header("X-Content-Type-Options", "nosniff")
            self.end_headers()
            self.wfile.write(body)

        def dispatch(self):
            if self.command == "GET" and self.path == "/health":
                return self.reply(200, {"ok": True})
            try:
                if self.headers.get("Transfer-Encoding"):
                    raise Invalid("Transfer encoding is unsupported")
                length = int(self.headers.get("Content-Length", 0))
                if not 0 <= length <= MAX_BODY:
                    raise Invalid("Request exceeds 1 MiB", 413)
                if not secret or not hmac.compare_digest(secret, self.headers.get("X-Pero-App-Token", "")):
                    raise Invalid("Host proxy authentication required", 401)
                self.connection.settimeout(15)
                body = self.rfile.read(length)
                if len(body) != length:
                    raise Invalid("Incomplete request body")
                data = {}
                if self.command == "POST":
                    if not self.headers.get("Content-Type", "").startswith("application/json"):
                        raise Invalid("JSON is required", 415)
                    data = mapping(json.loads(body), "request")
                parsed = urlsplit(self.path)
                path = parsed.path
                if self.command == "GET":
                    if path == "/api/library":
                        return self.reply(200, store.library())
                    if path == "/api/example":
                        return self.reply(200, {
                            "title": "Synthetic reading · a reproducible citation example",
                            "sourceId": "synthetic-evidence-example-v1",
                            "url": "https://example.invalid/synthetic-paper",
                            "authors": ["Pero synthetic fixture"],
                            "contentKind": "full-text",
                            "content": "# Synthetic citation example\n\n## Method\nThis is a synthetic paper fixture, not published research.\n\nA source snapshot preserves the exact text used for a report.\n\n## Results\nNo experiment was performed. Any report about this example must retain that limitation.\n",
                        })
                    match = re.fullmatch(r"/api/(papers|intents|reports)/([a-f0-9]{64})(/export)?", path)
                    if match:
                        kind, identity, exporting = match.groups()
                        if exporting:
                            if kind != "reports":
                                raise Invalid("Only reports can be exported", 404)
                            return self.reply(200, store.export(identity, parse_qs(parsed.query).get("format", ["markdown"])[0]))
                        record = {"papers": store.paper, "intents": store.intent, "reports": store.report}[kind](identity)
                        return self.reply(200, record.to_dict())
                if self.command == "POST":
                    if path == "/api/search":
                        return self.reply(200, literature.search(data))
                    if path == "/api/papers":
                        paper = PaperSnapshot.from_reading(literature.read(data), now())
                        return self.reply(201, store.save_paper(paper).to_dict())
                    if path == "/api/papers/import":
                        paper = PaperSnapshot.from_reading({**data, "source": "import"}, now())
                        return self.reply(201, store.save_paper(paper).to_dict())
                    if path == "/api/intents":
                        return self.reply(201, store.prepare(ResearchRequest.parse(data), now()).to_dict())
                    if path == "/api/reports":
                        saved = store.save_report(data.get("intentId"), data.get("text"), now(), data.get("sessionId"))
                        return self.reply(201, saved.to_dict())
                    if path == "/api/evidence":
                        intent = store.intent(data.get("intentId"))
                        cited = tuple(paper.id for paper in intent.papers)
                        evidence = EvidenceReference.parse_many([{"paperId": data.get("paperId"), "quote": data.get("quote")}], cited, intent.papers)[0]
                        return self.reply(200, evidence.locate(intent.papers))
                raise Invalid("Route not found", 404)
            except Invalid as error:
                self.reply(error.status, {"code": "research_invalid", "detail": error.detail})
            except (ValueError, TypeError, KeyError, UnicodeError, RecursionError) as error:
                self.reply(400, {"code": "request_invalid", "detail": str(error) or "Invalid request"})
            except OSError:
                self.reply(500, {"code": "backend_io_error", "detail": "Cannot read or write the research data directory"})

        do_GET = do_POST = dispatch

    return Handler
