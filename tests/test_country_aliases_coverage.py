import json
import sys
import types
import unittest
from pathlib import Path


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
            pass

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


class CountryAliasesCoverageTests(unittest.TestCase):
    def test_new_aliases_are_resolved(self):
        cases = {
            "U.S.": "US",
            "U.K.": "GB",
            "P.R.C.": "CN",
            "U.A.E.": "AE",
            "Viet Nam": "VN",
            "Turkiye": "TR",
            "Congo-Kinshasa": "CD",
            "美國": "US",
            "英國": "GB",
            "南韓": "KR",
        }
        for raw, expected in cases.items():
            with self.subTest(raw=raw):
                self.assertEqual(server.get_country_code_alpha2(raw), expected)

    def test_new_area_codes_exist_and_resolve(self):
        cases = {
            "Aland Islands": "AX",
            "Åland Islands": "AX",
            "Caribbean Netherlands": "BQ",
            "Curacao": "CW",
            "Curaçao": "CW",
        }
        for raw, expected in cases.items():
            with self.subTest(raw=raw):
                self.assertEqual(server.get_country_code_alpha2(raw), expected)

    def test_unknown_name_still_falls_back_to_us(self):
        self.assertEqual(server.get_country_code_alpha2("unknownland"), "US")
        self.assertEqual(server.get_country_code_alpha2(None), "US")
        self.assertEqual(server.get_country_code_alpha2(""), "US")

    def test_aliases_have_no_in_country_duplicates(self):
        data_path = Path("firecrawl_toolkit/data/country_aliases.json")
        data = json.loads(data_path.read_text(encoding="utf-8"))
        for code, names in data.items():
            with self.subTest(code=code):
                self.assertEqual(len(names), len(set(names)))


if __name__ == "__main__":
    unittest.main()
