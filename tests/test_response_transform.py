import json
import sys
import types
import unittest


def _install_test_stubs_if_needed():
    if "httpx" not in sys.modules:
        httpx_stub = types.ModuleType("httpx")

        class _DummyAsyncClient:
            def __init__(self, *args, **kwargs):
                pass

            async def post(self, *args, **kwargs):
                raise RuntimeError("httpx stub should not execute network calls in tests")

            async def aclose(self):
                return None

        class _DummyLimits:
            def __init__(self, *args, **kwargs):
                pass

        class _DummyTimeout:
            def __init__(self, *args, **kwargs):
                pass

        class _DummyHTTPStatusError(Exception):
            def __init__(self, *args, **kwargs):
                super().__init__(*args)
                self.response = kwargs.get("response")
                self.request = kwargs.get("request")

        class _DummyRequestError(Exception):
            pass

        httpx_stub.AsyncClient = _DummyAsyncClient
        httpx_stub.Limits = _DummyLimits
        httpx_stub.Timeout = _DummyTimeout
        httpx_stub.HTTPStatusError = _DummyHTTPStatusError
        httpx_stub.RequestError = _DummyRequestError
        sys.modules["httpx"] = httpx_stub

    if "dotenv" not in sys.modules:
        dotenv_stub = types.ModuleType("dotenv")
        dotenv_stub.load_dotenv = lambda *args, **kwargs: None
        sys.modules["dotenv"] = dotenv_stub

    if "fastmcp" not in sys.modules:
        fastmcp_stub = types.ModuleType("fastmcp")

        class _DummyFastMCP:
            def __init__(self, *args, **kwargs):
                pass

            def tool(self, name=None):
                def _decorator(func):
                    return func

                return _decorator

            async def run_async(self, *args, **kwargs):
                return None

        fastmcp_stub.FastMCP = _DummyFastMCP
        sys.modules["fastmcp"] = fastmcp_stub


_install_test_stubs_if_needed()
from firecrawl_toolkit import server


class ResponseTransformTests(unittest.IsolatedAsyncioTestCase):
    def test_transform_search_result_full_mapping(self):
        raw = {
            "success": True,
            "creditsUsed": 12,
            "data": {
                "web": [{"title": "w", "description": "wd", "url": "wu", "position": 1}],
                "news": [{"title": "n", "snippet": "ns", "url": "nu", "date": "4 days ago", "imageUrl": "x"}],
                "images": [{"title": "i", "imageUrl": "iu", "url": "ru", "imageWidth": 100}],
            },
        }

        transformed = server.transform_search_result(raw)
        self.assertEqual(
            transformed,
            {
                "success": True,
                "data": {
                    "web": [{"title": "w", "description": "wd", "url": "wu"}],
                    "news": [{"title": "n", "snippet": "ns", "url": "nu", "date": "4 days ago"}],
                    "images": [{"title": "i", "imageUrl": "iu", "url": "ru"}],
                },
                "creditsUsed": 12,
            },
        )

    def test_transform_search_result_empty_and_missing(self):
        transformed = server.transform_search_result({"success": False})
        self.assertEqual(transformed["success"], False)
        self.assertNotIn("country", transformed)
        self.assertEqual(transformed["data"]["web"], [])
        self.assertEqual(transformed["data"]["news"], [])
        self.assertEqual(transformed["data"]["images"], [])
        self.assertIsNone(transformed["creditsUsed"])

    def test_transform_search_result_news_snippet_missing(self):
        raw = {
            "success": True,
            "data": {
                "web": [],
                "news": [{"title": "n", "url": "nu", "date": "today"}],
                "images": [],
            },
            "creditsUsed": 1,
        }
        transformed = server.transform_search_result(raw)
        self.assertEqual(
            transformed["data"]["news"][0],
            {"title": "n", "snippet": None, "url": "nu", "date": "today"},
        )

    def test_transform_scrape_result_mapping_and_decode(self):
        raw = {
            "success": True,
            "data": {
                "markdown": "hello%20world%21",
                "metadata": {
                    "proxyUsed": "auto",
                    "title": "t",
                    "description": "d",
                    "language": "en",
                    "creditsUsed": 1,
                },
            },
        }
        transformed = server.transform_scrape_result(raw)
        self.assertEqual(
            transformed,
            {
                "success": True,
                "proxyUsed": "auto",
                "title": "t",
                "description": "d",
                "language": "en",
                "markdown": "hello world!",
                "creditsUsed": 1,
            },
        )

    def test_transform_scrape_result_missing_fields(self):
        transformed = server.transform_scrape_result({"success": False, "data": {}})
        self.assertEqual(transformed["success"], False)
        self.assertIsNone(transformed["proxyUsed"])
        self.assertIsNone(transformed["title"])
        self.assertIsNone(transformed["description"])
        self.assertIsNone(transformed["language"])
        self.assertIsNone(transformed["markdown"])
        self.assertIsNone(transformed["creditsUsed"])

    async def test_search_output_single_line_json(self):
        async def fake_execute(*_args, **_kwargs):
            return {
                "success": True,
                "creditsUsed": 2,
                "data": {"web": [], "news": [], "images": []},
            }

        old_execute = server.execute_firecrawl_request
        server.execute_firecrawl_request = fake_execute
        try:
            out = await server.firecrawl_search(query="ai")
        finally:
            server.execute_firecrawl_request = old_execute

        self.assertNotIn("\n", out)
        parsed = json.loads(out)
        self.assertNotIn("country", parsed)
        self.assertIn("data", parsed)

    async def test_scrape_error_output_single_line_json(self):
        out = await server.firecrawl_scrape(url="")
        self.assertNotIn("\n", out)
        parsed = json.loads(out)
        self.assertEqual(parsed["success"], False)
        self.assertTrue(parsed["error"])

    async def test_search_upstream_success_false_returns_error(self):
        async def fake_execute(*_args, **_kwargs):
            return {"success": False, "error": "upstream failure"}

        old_execute = server.execute_firecrawl_request
        server.execute_firecrawl_request = fake_execute
        try:
            out = await server.firecrawl_search(query="ai")
        finally:
            server.execute_firecrawl_request = old_execute

        parsed = json.loads(out)
        self.assertEqual(parsed["success"], False)
        self.assertTrue(parsed["error"])

    async def test_scrape_upstream_success_false_returns_error(self):
        async def fake_execute(*_args, **_kwargs):
            return {"success": False, "error": "upstream failure"}

        old_execute = server.execute_firecrawl_request
        server.execute_firecrawl_request = fake_execute
        try:
            out = await server.firecrawl_scrape(url="https://example.com")
        finally:
            server.execute_firecrawl_request = old_execute

        parsed = json.loads(out)
        self.assertEqual(parsed["success"], False)
        self.assertTrue(parsed["error"])


if __name__ == "__main__":
    unittest.main()
