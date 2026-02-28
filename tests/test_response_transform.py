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

    async def test_scrape_exclude_tags_normalize_merge_and_escape_safe(self):
        calls = []

        async def fake_execute(_api_url, payload, _api_name):
            calls.append(payload)
            return {"success": True, "data": {"markdown": "ok", "metadata": {}}}

        old_execute = server.execute_firecrawl_request
        server.execute_firecrawl_request = fake_execute
        try:
            out = await server.firecrawl_scrape(
                url="https://example.com",
                excludeTags=[
                    "  ",
                    None,
                    123,
                    "[class^=\\\"skip\\\"]",
                    "[id*=\\\"disqus\\\"]",
                    "[id*=\\\"disqus\\\"]",
                ],
            )
        finally:
            server.execute_firecrawl_request = old_execute

        parsed = json.loads(out)
        self.assertEqual(parsed["success"], True)
        self.assertEqual(len(calls), 1)
        sent_exclude_tags = calls[0]["excludeTags"]
        self.assertIn("script", sent_exclude_tags)
        self.assertIn("[class^=\\\"skip\\\"]", sent_exclude_tags)
        self.assertEqual(sent_exclude_tags.count("[id*=\\\"disqus\\\"]"), 1)

    async def test_scrape_empty_markdown_triggers_fallback_without_tag_keys(self):
        calls = []

        async def fake_execute(_api_url, payload, _api_name):
            calls.append(payload)
            if len(calls) == 1:
                return {"success": True, "data": {"markdown": "", "metadata": {}}}
            return {"success": True, "data": {"markdown": "fallback", "metadata": {}}}

        old_execute = server.execute_firecrawl_request
        server.execute_firecrawl_request = fake_execute
        try:
            out = await server.firecrawl_scrape(url="https://example.com")
        finally:
            server.execute_firecrawl_request = old_execute

        parsed = json.loads(out)
        self.assertEqual(parsed["success"], True)
        self.assertEqual(parsed["markdown"], "fallback")
        self.assertEqual(len(calls), 2)
        self.assertIn("includeTags", calls[0])
        self.assertIn("excludeTags", calls[0])
        self.assertNotIn("includeTags", calls[1])
        self.assertNotIn("excludeTags", calls[1])

    async def test_scrape_none_markdown_does_not_trigger_fallback(self):
        calls = []

        async def fake_execute(_api_url, payload, _api_name):
            calls.append(payload)
            return {"success": True, "data": {"markdown": None, "metadata": {}}}

        old_execute = server.execute_firecrawl_request
        server.execute_firecrawl_request = fake_execute
        try:
            out = await server.firecrawl_scrape(url="https://example.com")
        finally:
            server.execute_firecrawl_request = old_execute

        parsed = json.loads(out)
        self.assertEqual(parsed["success"], True)
        self.assertIsNone(parsed["markdown"])
        self.assertEqual(len(calls), 1)

    async def test_scrape_max_characters_truncates_markdown(self):
        async def fake_execute(*_args, **_kwargs):
            return {"success": True, "data": {"markdown": "hello world", "metadata": {}}}

        old_execute = server.execute_firecrawl_request
        server.execute_firecrawl_request = fake_execute
        try:
            out = await server.firecrawl_scrape(url="https://example.com", maxCharacters=5)
        finally:
            server.execute_firecrawl_request = old_execute

        parsed = json.loads(out)
        self.assertEqual(parsed["success"], True)
        self.assertEqual(parsed["markdown"], "hello")

    async def test_scrape_max_characters_equal_or_greater_than_length(self):
        async def fake_execute(*_args, **_kwargs):
            return {"success": True, "data": {"markdown": "hello", "metadata": {}}}

        old_execute = server.execute_firecrawl_request
        server.execute_firecrawl_request = fake_execute
        try:
            out_equal = await server.firecrawl_scrape(url="https://example.com", maxCharacters=5)
            out_greater = await server.firecrawl_scrape(url="https://example.com", maxCharacters=10)
        finally:
            server.execute_firecrawl_request = old_execute

        parsed_equal = json.loads(out_equal)
        parsed_greater = json.loads(out_greater)
        self.assertEqual(parsed_equal["markdown"], "hello")
        self.assertEqual(parsed_greater["markdown"], "hello")

    async def test_scrape_max_characters_invalid_values_are_ignored(self):
        async def fake_execute(*_args, **_kwargs):
            return {"success": True, "data": {"markdown": "hello world", "metadata": {}}}

        old_execute = server.execute_firecrawl_request
        server.execute_firecrawl_request = fake_execute
        try:
            out_zero = await server.firecrawl_scrape(url="https://example.com", maxCharacters=0)
            out_negative = await server.firecrawl_scrape(url="https://example.com", maxCharacters=-1)
            out_non_int = await server.firecrawl_scrape(url="https://example.com", maxCharacters="5")
        finally:
            server.execute_firecrawl_request = old_execute

        parsed_zero = json.loads(out_zero)
        parsed_negative = json.loads(out_negative)
        parsed_non_int = json.loads(out_non_int)
        self.assertEqual(parsed_zero["markdown"], "hello world")
        self.assertEqual(parsed_negative["markdown"], "hello world")
        self.assertEqual(parsed_non_int["markdown"], "hello world")

    async def test_scrape_max_characters_keeps_none_markdown(self):
        async def fake_execute(*_args, **_kwargs):
            return {"success": True, "data": {"markdown": None, "metadata": {}}}

        old_execute = server.execute_firecrawl_request
        server.execute_firecrawl_request = fake_execute
        try:
            out = await server.firecrawl_scrape(url="https://example.com", maxCharacters=5)
        finally:
            server.execute_firecrawl_request = old_execute

        parsed = json.loads(out)
        self.assertIsNone(parsed["markdown"])

    async def test_scrape_fallback_failure_keeps_first_result(self):
        calls = []

        async def fake_execute(_api_url, payload, _api_name):
            calls.append(payload)
            if len(calls) == 1:
                return {"success": True, "data": {"markdown": "", "metadata": {}}}
            return {"error": True, "message": "fallback failed"}

        old_execute = server.execute_firecrawl_request
        server.execute_firecrawl_request = fake_execute
        try:
            out = await server.firecrawl_scrape(url="https://example.com")
        finally:
            server.execute_firecrawl_request = old_execute

        parsed = json.loads(out)
        self.assertEqual(parsed["success"], True)
        self.assertEqual(parsed["markdown"], "")
        self.assertEqual(len(calls), 2)

    async def test_scrape_fallback_result_respects_max_characters(self):
        calls = []

        async def fake_execute(_api_url, payload, _api_name):
            calls.append(payload)
            if len(calls) == 1:
                return {"success": True, "data": {"markdown": "", "metadata": {}}}
            return {"success": True, "data": {"markdown": "fallback content", "metadata": {}}}

        old_execute = server.execute_firecrawl_request
        server.execute_firecrawl_request = fake_execute
        try:
            out = await server.firecrawl_scrape(url="https://example.com", maxCharacters=8)
        finally:
            server.execute_firecrawl_request = old_execute

        parsed = json.loads(out)
        self.assertEqual(parsed["success"], True)
        self.assertEqual(parsed["markdown"], "fallback")
        self.assertEqual(len(calls), 2)


if __name__ == "__main__":
    unittest.main()
