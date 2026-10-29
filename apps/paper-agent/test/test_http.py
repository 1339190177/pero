"""One loopback check of the App transport and domain persistence wiring."""
from http.client import HTTPConnection
from http.server import ThreadingHTTPServer
import json
import tempfile
import threading
import unittest
from test_research import NOW, reading, report_value
from paper_agent.store import ResearchStore


class LiteratureFixture:
    def search(self, _query):
        return dict(items=[dict(id="1706.03762", source="alphaxiv", title="Synthetic retrieval")], warnings=[])

    def read(self, _query):
        return reading()


class ResearchHTTP(unittest.TestCase):
    def test_authenticated_local_workflow(self):
        from paper_agent.http import handler
        with tempfile.TemporaryDirectory() as root:
            store = ResearchStore(root)
            server = ThreadingHTTPServer(("127.0.0.1", 0), handler(store, "test-token", LiteratureFixture(), lambda: NOW))
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()

            def call(method, path, body=None, token="test-token"):
                connection = HTTPConnection("127.0.0.1", server.server_port, timeout=3)
                payload = json.dumps(body) if body is not None else None
                connection.request(method, path, body=payload, headers={"Content-Type": "application/json", "X-Pero-App-Token": token})
                response = connection.getresponse()
                result = json.loads(response.read())
                status = response.status
                connection.close()
                return status, result

            try:
                self.assertEqual(call("GET", "/api/library", token="wrong")[0], 401)
                example = call("GET", "/api/example")[1]
                self.assertIn("Synthetic", example["title"])
                self.assertEqual(call("GET", "/api/library")[1]["papers"], [])
                paper = call("POST", "/api/papers/import", reading())[1]
                self.assertEqual(paper["source"], "import")
                self.assertEqual(call("GET", "/api/papers/" + paper["id"])[1]["content"], reading()["content"])
                self.assertEqual(call("POST", "/api/search", dict(query="attention"))[1]["items"][0]["source"], "alphaxiv")
                frozen = call("POST", "/api/intents", dict(question="Evidence?", paperIds=[paper["id"]], requestId="http-request", verifyCode=False))[1]
                self.assertIn("untrusted", frozen["prompt"])
                intent = store.intent(frozen["id"])
                status, saved = call("POST", "/api/reports", dict(intentId=frozen["id"], text=json.dumps(report_value(intent))))
                self.assertEqual(status, 201)
                self.assertEqual(saved["acceptance"], "unverified")
                self.assertEqual(call("GET", "/api/reports/" + saved["id"] + "/export?format=markdown")[0], 200)
                located = call("POST", "/api/evidence", dict(intentId=frozen["id"], paperId=paper["id"], quote="Exact original evidence."))[1]
                self.assertEqual(located["quote"], "Exact original evidence.")
                self.assertEqual(call("POST", "/api/evidence", dict(intentId=frozen["id"], paperId=paper["id"], quote="Fake quotation"))[0], 400)
                self.assertEqual(len(call("GET", "/api/library")[1]["reports"]), 1)
            finally:
                server.shutdown()
                server.server_close()
                thread.join(timeout=3)


if __name__ == "__main__":
    unittest.main()
