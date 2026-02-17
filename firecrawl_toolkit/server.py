import os
import tempfile
import httpx
import asyncio
import json
import logging
import re
import unicodedata
import random
import threading
from urllib.parse import unquote
from typing import Optional, Dict, Any, Union, List
from dotenv import load_dotenv
from fastmcp import FastMCP
from concurrent.futures import ThreadPoolExecutor

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

load_dotenv()

m = None
try:
    import httpx as _httpx  # type: ignore
    m = _httpx
except Exception:
    m = None

API_KEY: Optional[str] = os.getenv("FIRECRAWL_API_KEY")
mcp = FastMCP("firecrawl-mcp")

# 加载本地国家别名字典 (data/country_aliases.json)
# 简化索引：仅使用别名字典 ALIAS_MAP，并生成按字母序排列的别名列表 ALIAS_KEYS_SORTED。
# 使用 Python 内置的 `sorted()` 对别名键进行排序。
ALIAS_MAP: Dict[str, str] = {}
ALIAS_KEYS_SORTED: list = []

_aliases_path = os.path.join(os.path.dirname(__file__), "data", "country_aliases.json")

# Quick Sort 实现（用于对别名字典键排序）

# 二分查找（在已排序的列表中查找精确匹配）
def binary_search(arr: list, target: str) -> Optional[int]:
    lo, hi = 0, len(arr) - 1
    while lo <= hi:
        mid = (lo + hi) // 2
        if arr[mid] == target:
            return mid
        if arr[mid] < target:
            lo = mid + 1
        else:
            hi = mid - 1
    return None

def normalize(text: str) -> str:
    """归一化国家/地区名称：NFKD、去重音、转小写、去标点、折叠空白"""
    if not text:
        return ""
    # Unicode normalize
    s = unicodedata.normalize("NFKD", text)
    # remove diacritics
    s = "".join(ch for ch in s if not unicodedata.combining(ch))
    # convert full-width to half-width and normalize spaces
    s = s.replace("\u3000", " ")
    s = s.strip().lower()
    # remove punctuation except spaces
    s = re.sub(r"[^\w\s'-]", " ", s, flags=re.UNICODE)
    # replace underscores and multiple spaces with single space
    s = re.sub(r"[_\s]+", " ", s).strip()
    return s

def _generate_variants(alias: str) -> set:
    """为 alias 生成若干变体以提高命中率（去标点、逗号重排等）"""
    variants = set()
    base = alias.strip()
    variants.add(base)
    # normalized base
    n = normalize(base)
    variants.add(n)
    # remove punctuation version
    variants.add(re.sub(r"[^\w\s]", "", n))
    # if contains comma, try reorder segments: "Korea, South" -> "south korea"
    if "," in base:
        parts = [p.strip() for p in base.split(",") if p.strip()]
        if len(parts) >= 2:
            reordered = " ".join(reversed(parts))
            variants.add(reordered)
            variants.add(normalize(reordered))
    # also add word-reordered variants for simple two-word names
    parts = n.split()
    if len(parts) == 2:
        variants.add(" ".join(reversed(parts)))
    return {v for v in variants if v}

try:
    with open(_aliases_path, "r", encoding="utf-8") as f:
        _forward = json.load(f)

    # 仅基于别名字典构建 ALIAS_MAP（normalized alias -> alpha2）
    for code, names in _forward.items():
        code_up = code.upper()
        if isinstance(names, list):
            iter_names = names
        else:
            iter_names = [names]
        for name in iter_names:
            if not isinstance(name, str):
                continue
            for variant in _generate_variants(name):
                key = normalize(variant)
                if not key:
                    continue
                # 后来的同名别名以最后一个为准（覆盖），保持简单明了
                ALIAS_MAP[key] = code_up

    # 使用 Python 内置排序对别名字典的键进行排序，供二分查找使用
    ALIAS_KEYS_SORTED = sorted(ALIAS_MAP.keys())

except FileNotFoundError:
    logger.warning("国家别名字典未找到: %s", _aliases_path)
except Exception as e:
    logger.warning("加载国家别名字典失败: %s", e)

USER_AGENT = "firecrawl_client/1.0"
API_ENDPOINTS = {
    "search": "https://api.firecrawl.dev/v2/search",
    "scrape": "https://api.firecrawl.dev/v2/scrape",

}
HTTP_TIMEOUT = 30.0

# 并发与重试相关配置（可通过环境变量调整）
FIRECRAWL_MAX_CONNECTIONS = int(os.getenv("FIRECRAWL_MAX_CONNECTIONS", "200"))
FIRECRAWL_KEEPALIVE = int(os.getenv("FIRECRAWL_KEEPALIVE", "20"))
FIRECRAWL_HTTP2 = os.getenv("FIRECRAWL_HTTP2", "0") == "1"
# 如果启用了 HTTP/2，确保环境中安装了 h2；否则回退为 False，避免 httpx 抛出 ImportError
if FIRECRAWL_HTTP2:
    try:
        import h2  # noqa: F401
    except Exception:
        logger.warning("FIRECRAWL_HTTP2 设置为启用，但未检测到 'h2' 包。将自动禁用 HTTP/2（请安装 httpx[http2] 以启用）。")
        FIRECRAWL_HTTP2 = False

# Firecrawl 后端总体并发上限为每秒 300 请求；将默认全局并发上限适当调高为 200（可通过环境变量调整）
FIRECRAWL_MAX_CONCURRENT_REQUESTS = int(os.getenv("FIRECRAWL_MAX_CONCURRENT_REQUESTS", "200"))
FIRECRAWL_MAX_WORKERS = int(os.getenv("FIRECRAWL_MAX_WORKERS", "10"))
FIRECRAWL_RETRY_COUNT = int(os.getenv("FIRECRAWL_RETRY_COUNT", "3"))
FIRECRAWL_RETRY_BASE_DELAY = float(os.getenv("FIRECRAWL_RETRY_BASE_DELAY", "0.5"))

# per-endpoint 配置（通过环境变量传入 JSON 字符串，示例: '{"search":10,"scrape":2}'）
try:
    PER_ENDPOINT_MAX_CONCURRENT = json.loads(os.getenv("FIRECRAWL_ENDPOINT_CONCURRENCY", "{}"))
except Exception:
    PER_ENDPOINT_MAX_CONCURRENT = {}

# per-endpoint 是否允许重试，默认允许（用于避免对非幂等接口重试）
try:
    PER_ENDPOINT_ALLOW_RETRY = json.loads(os.getenv("FIRECRAWL_ENDPOINT_RETRYABLE", '{"search": true, "scrape": false}'))
except Exception:
    PER_ENDPOINT_ALLOW_RETRY = {}

# 请求并发信号量（在 startup_all 时初始化）
REQUEST_SEMAPHORE = None

# endpoint -> asyncio.Semaphore 映射（在 startup_all 中根据 PER_ENDPOINT_MAX_CONCURRENT 初始化）
ENDPOINT_SEMAPHORES: Dict[str, asyncio.Semaphore] = {}

# 全局 httpx AsyncClient 管理类
class AsyncHttpClientManager:
    # 当 httpx 不可用时，推迟到运行时抛出更明确的错误
    _client: Optional["httpx.AsyncClient"] = None
    _lock = asyncio.Lock()

    @classmethod
    async def startup(cls):
        async with cls._lock:
            if cls._client is None:
                limits = httpx.Limits(
                    max_connections=FIRECRAWL_MAX_CONNECTIONS,
                    max_keepalive_connections=FIRECRAWL_KEEPALIVE
                )
                timeout_obj = httpx.Timeout(
                    connect=5.0,
                    read=20.0,
                    write=10.0,
                    pool=30.0,
                    timeout=HTTP_TIMEOUT
                )
                if m is None:
                    raise RuntimeError("依赖库 'httpx' 未安装，请先安装 httpx")
                cls._client = m.AsyncClient(
                    timeout=timeout_obj,
                    headers={"User-Agent": USER_AGENT},
                    limits=limits,
                    http2=FIRECRAWL_HTTP2
                )
                logger.info("httpx AsyncClient 已启动 (max_connections=%d, keepalive=%d, http2=%s, timeout=%s)",
                            FIRECRAWL_MAX_CONNECTIONS, FIRECRAWL_KEEPALIVE, FIRECRAWL_HTTP2, HTTP_TIMEOUT)

    @classmethod
    def get_client(cls) -> httpx.AsyncClient:
        if cls._client is None:
            raise RuntimeError("AsyncHttpClientManager 未启动，请先调用startup()")
        return cls._client

    @classmethod
    async def shutdown(cls):
        async with cls._lock:
            if cls._client:
                await cls._client.aclose()
                logger.info("httpx AsyncClient 已关闭")
                cls._client = None


# 全局线程池执行器管理
class ThreadPoolManager:
    _executor: Optional[ThreadPoolExecutor] = None
    _max_workers = FIRECRAWL_MAX_WORKERS

    @classmethod
    def startup(cls, max_workers: int = 10):
        if cls._executor is None:
            cls._executor = ThreadPoolExecutor(max_workers=cls._max_workers)
            logger.info(f"线程池启动，最大工作线程数: {cls._max_workers}")

    @classmethod
    def get_executor(cls) -> ThreadPoolExecutor:
        if cls._executor is None:
            raise RuntimeError("ThreadPoolManager 未启动，请先调用startup()")
        return cls._executor

    @classmethod
    def shutdown(cls):
        if cls._executor:
            cls._executor.shutdown(wait=True)
            logger.info("线程池已关闭")
            cls._executor = None


def to_compact_json(payload: Dict[str, Any]) -> str:
    return json.dumps(payload, ensure_ascii=False, separators=(",", ":"))


def pick(d: Dict[str, Any], path: List[str], default=None):
    current: Any = d
    for key in path:
        if not isinstance(current, dict):
            return default
        if key not in current:
            return default
        current = current[key]
    return current


def compact_error_response(message: str, status_code: Optional[int] = None, extra: Optional[Dict[str, Any]] = None) -> str:
    result: Dict[str, Any] = {
        "success": False,
        "error": True,
        "message": message,
    }
    if status_code is not None:
        result["status_code"] = status_code
    if extra:
        result.update(extra)
    return to_compact_json(result)


def error_response(message: str, status_code: Optional[int] = None, extra: Optional[Dict[str, Any]] = None) -> str:
    return compact_error_response(message, status_code, extra)


def success_response(query_details: Dict[str, Any], results: Dict[str, Any]) -> str:
    return json.dumps({
        "success": True,
        "query_details": query_details,
        "results": results,
    }, ensure_ascii=False, indent=4)


def transform_search_result(raw: Dict[str, Any]) -> Dict[str, Any]:
    def map_items(items: Any, fields: List[str]) -> List[Dict[str, Any]]:
        if not isinstance(items, list):
            return []
        mapped: List[Dict[str, Any]] = []
        for item in items:
            if isinstance(item, dict):
                mapped.append({field: item.get(field, None) for field in fields})
            else:
                mapped.append({field: None for field in fields})
        return mapped

    return {
        "success": pick(raw, ["success"], None),
        "data": {
            "web": map_items(pick(raw, ["data", "web"], []), ["title", "description", "url"]),
            "news": map_items(pick(raw, ["data", "news"], []), ["title", "snippet", "url", "date"]),
            "images": map_items(pick(raw, ["data", "images"], []), ["title", "imageUrl", "url"]),
        },
        "creditsUsed": pick(raw, ["creditsUsed"], None),
    }


def transform_scrape_result(raw: Dict[str, Any]) -> Dict[str, Any]:
    markdown_val = pick(raw, ["data", "markdown"], None)
    decoded_markdown = unquote(markdown_val) if isinstance(markdown_val, str) else None

    return {
        "success": pick(raw, ["success"], None),
        "proxyUsed": pick(raw, ["data", "metadata", "proxyUsed"], None),
        "title": pick(raw, ["data", "metadata", "title"], None),
        "description": pick(raw, ["data", "metadata", "description"], None),
        "language": pick(raw, ["data", "metadata", "language"], None),
        "markdown": decoded_markdown,
        "creditsUsed": pick(raw, ["data", "metadata", "creditsUsed"], None),
    }


def get_country_code_alpha2(country_name: Optional[str]) -> str:
    """
    国家代码解析（已简化）：
    - 仅基于别名字典 ALIAS_MAP 进行查找，使用 ALIAS_KEYS_SORTED + 二分查找匹配别名键。
    - 优先在别名字典中查找传入参数（归一化后）；若命中则返回对应 alpha2。
    - 如果传入为两字母 ISO2 则作为后备直接返回大写。
    - 未找到则默认返回 'US'。
    """
    # 处理空输入：直接使用默认 US
    if not country_name:
        return "US"

    name = country_name.strip()
    if not name:
        return "US"

    # 归一化并首先在 ALIAS_MAP 中查找（O(1)）
    norm = normalize(name)
    if norm in ALIAS_MAP:
        return ALIAS_MAP[norm]

    # 对大规模别名列表，使用已排序键列表 + 二分查找
    if ALIAS_KEYS_SORTED:
        idx = binary_search(ALIAS_KEYS_SORTED, norm)
        if idx is not None:
            return ALIAS_MAP.get(ALIAS_KEYS_SORTED[idx], "US")

    # 如果已经是两字母 ISO2，作为后备直接返回大写
    if len(name) == 2 and name.isalpha():
        return name.upper()

    # 再尝试归一化的大写形式（例如传入 'USA'）
    u_norm = normalize(name.upper())
    if u_norm in ALIAS_MAP:
        return ALIAS_MAP[u_norm]
    if ALIAS_KEYS_SORTED:
        idx = binary_search(ALIAS_KEYS_SORTED, u_norm)
        if idx is not None:
            return ALIAS_MAP.get(ALIAS_KEYS_SORTED[idx], "US")

    logger.info("未找到国家名称 '%s'，使用默认国家码 US。", country_name)
    return "US"


def validate_search_num(num_val: int) -> int:
    if 1 <= num_val <= 100:
        return num_val
    logger.warning("搜索数量 %d 超出范围(1-100)，使用默认值20。", num_val)
    return 20


def map_search_time_to_tbs_param(time_period_str: Optional[str]) -> Optional[str]:
    """
    将各种用户友好的时间范围字符串规范化为 Google tbs 参数格式之一：
    qdr:h, qdr:d, qdr:w, qdr:m, qdr:y。

    支持的输入示例（中/英常见写法、缩写、带数字的写法等）：
      "hour", "hours", "小时", "24h", "过去24小时" -> qdr:h
      "day", "days", "天", "过去7天", "last 7 days" -> qdr:d
      "week", "weeks", "周", "最近一周" -> qdr:w
      "month", "months", "月", "最近一月" -> qdr:m
      "year", "years", "年", "最近一年" -> qdr:y

    规则说明：
    - 若传入已是合法的 qdr: 值（qdr:h/d/w/m/y），原样返回。
    - 使用按优先级（年->月->周->天->小时）匹配的正则规则集进行识别，匹配到就返回对应值。
    - 未识别的输入返回 None（表示不设置 tbs）。
    """
    if not time_period_str:
        return None

    s = time_period_str.strip().lower()

    # 如果已经是 qdr: 开头且是允许的值，直接返回（否则忽略）
    allowed_qdr = {"qdr:h", "qdr:d", "qdr:w", "qdr:m", "qdr:y"}
    if s.startswith("qdr:"):
        return s if s in allowed_qdr else None

    # 按优先级匹配：年 -> 月 -> 周 -> 天 -> 小时
    # 各模式支持中/英、缩写、复数、带数字的写法（如 "24h", "7天"）以及口语化表达（"last week", "最近一周"）
    year_re = r"(?:\b(?:y|yr|yrs|year|years)\b|年|last\s+year|past\s+year|最近\s*\d*\s*年|过去\s*\d*\s*年|\d+\s*y\b|\d+\s*年)"
    month_re = r"(?:\b(?:mo|mos|month|months)\b|月|last\s+month|past\s+month|最近\s*\d*\s*月|过去\s*\d*\s*月|\d+\s*mo\b|\d+\s*月)"
    week_re = r"(?:\b(?:w|wk|wks|week|weeks)\b|周|星期|last\s+week|past\s+week|最近\s*\d*\s*周|过去\s*\d*\s*周|\d+\s*w\b|\d+\s*周)"
    day_re = r"(?:\b(?:d|day|days)\b|天|日|daily|每?天|last\s+day|past\s+day|最近\s*\d*\s*天|过去\s*\d*\s*天|\d+\s*d\b|\d+\s*天)"
    hour_re = r"(?:\b(?:h|hr|hrs|hour|hours)\b|小时|小时内|past\s+hour|last\s+hour|最近\s*\d*\s*小时|过去\s*\d*\s*小时|\d+\s*h\b|\d+\s*小时)"

    try:
        if re.search(year_re, s, flags=re.IGNORECASE):
            return "qdr:y"
        if re.search(month_re, s, flags=re.IGNORECASE):
            return "qdr:m"
        if re.search(week_re, s, flags=re.IGNORECASE):
            return "qdr:w"
        if re.search(day_re, s, flags=re.IGNORECASE):
            return "qdr:d"
        if re.search(hour_re, s, flags=re.IGNORECASE):
            return "qdr:h"
    except re.error:
        # 万一正则出错，记录并返回 None
        logger.exception("时间范围匹配正则错误，输入: %s", time_period_str)
        return None

    logger.info("未识别的时间偏好 '%s'，忽略时间过滤。", time_period_str)
    return None


async def execute_firecrawl_request(
    api_url: str,
    payload: Dict[str, Any],
    api_name: str
) -> Union[Dict[str, Any], None]:
    """
    执行对 Firecrawl API 的异步请求，包含并发控制与重试策略。
    返回解析后的 JSON 或错误描述字典，或 None（当缺少 API_KEY 时）。
    """
    if not API_KEY:
        logger.error("未配置FIRECRAWL_API_KEY，无法调用 %s 接口。", api_name)
        return None

    headers = {
        "Authorization": f"Bearer {API_KEY}",
        "Content-Type": "application/json",
    }

    try:
        client = AsyncHttpClientManager.get_client()
    except RuntimeError as e:
        logger.error("HTTP客户端未启动: %s", e)
        return {"error": True, "message": f"{api_name}接口请求失败，HTTP客户端未启动。"}

    logger.info("准备调用 %s 接口，payload: %s", api_name, payload)

    # 选择用于该端点的 semaphore（优先 per-endpoint，其次全局 REQUEST_SEMAPHORE）
    sem = ENDPOINT_SEMAPHORES.get(api_name, REQUEST_SEMAPHORE)
    # 该端点是否允许重试（默认 True）
    retry_allowed = PER_ENDPOINT_ALLOW_RETRY.get(api_name, True)

    attempt = 0
    while True:
        try:
            # 并发控制（如果已在 startup_all 中初始化）
            if REQUEST_SEMAPHORE:
                async with REQUEST_SEMAPHORE:
                    response = await client.post(api_url, json=payload, headers=headers)
            else:
                response = await client.post(api_url, json=payload, headers=headers)

            # 检查 HTTP 状态
            response.raise_for_status()

            # 解析返回 JSON
            try:
                result = response.json()
            except Exception as e:
                logger.error("%s接口JSON解析错误: %s", api_name, e)
                return {"error": True, "message": f"{api_name}接口响应解析失败: {e}"}

            return result

        except httpx.HTTPStatusError as e:
            status = e.response.status_code if e.response is not None else None
            logger.warning("%s 接口返回 HTTP 错误 %s (尝试 %d/%d)", api_name, status, attempt + 1, FIRECRAWL_RETRY_COUNT)
            # 对 5xx 做重试（仅在端点允许重试时）
            if retry_allowed and status and 500 <= status < 600 and attempt < FIRECRAWL_RETRY_COUNT:
                attempt += 1
                delay = FIRECRAWL_RETRY_BASE_DELAY * (2 ** (attempt - 1)) + random.uniform(0, 0.1)
                logger.info("对 %s 接口在 %s 秒后重试 (HTTP %s)", api_name, round(delay, 2), status)
                await asyncio.sleep(delay)
                continue
            logger.error("%s接口HTTP错误 %s %s: %s", api_name, status, e.request.url if e.request else "", e)
            return {
                "error": True,
                "message": f"{api_name}接口HTTP状态错误: {status}",
                "status_code": status
            }

        except httpx.RequestError as e:
            logger.warning("%s 接口请求错误 (尝试 %d/%d): %s", api_name, attempt + 1, FIRECRAWL_RETRY_COUNT, e)
            if retry_allowed and attempt < FIRECRAWL_RETRY_COUNT:
                attempt += 1
                delay = FIRECRAWL_RETRY_BASE_DELAY * (2 ** (attempt - 1)) + random.uniform(0, 0.1)
                logger.info("对 %s 接口在 %s 秒后重试 (请求错误)", api_name, round(delay, 2))
                await asyncio.sleep(delay)
                continue
            logger.error("%s接口请求错误: %s", api_name, e)
            return {"error": True, "message": f"{api_name}接口请求错误: {e}"}

        except Exception as e:
            logger.exception("%s接口未知错误: %s", api_name, e)
            return {"error": True, "message": f"{api_name}接口未知错误: {e}"}


@mcp.tool(name="firecrawl-aggregated-search")
async def firecrawl_search(
    query: str,
    country: Optional[str] = None,
    search_num: int = 20,
    search_time: Optional[str] = None,
) -> str:
    """
    Aggregated Search Interface, Combining Webpage, News, And Image Search Results.
    Parameters:
        query: Search Keywords (Required)
        search_num: Number Of Results To Return, 1~100, Default 20 (Required)
        country: Country Name (Optional)
        search_time: Time Filter, Such As "hour", "day", "week", "month", "year" (Optional)
    Returns:
        Json Format String
    """
    api_name_key = "search"
    api_url = API_ENDPOINTS.get(api_name_key)
    if not api_url:
        return compact_error_response(f"未知API_KEY '{api_name_key}'，无法处理请求。")

    # 解析并规范化国家代码为 ISO2 大写
    country_code = get_country_code_alpha2(country)
    logger.info("search country param '%s' -> resolved country_code: %s", country, country_code)

    # 构建固定的 payload
    payload: Dict[str, Any] = {
        "query": query,
        "limit": validate_search_num(search_num),
        "sources": [{"type": "web"}, {"type": "news"}, {"type": "images"}],
        # tbs 可选：根据传入的 search_time 映射
        "country": country_code,
        "timeout": 60000,
        "ignoreInvalidURLs": False,
        "scrapeOptions": {
            "formats": [],
            "onlyMainContent": True,
            "maxAge": 172800000,
            "waitFor": 0,
            "mobile": False,
            "skipTlsVerification": False,
            "timeout": 30000,
            "parsers": [],
            "location": {
                "country": country_code
            },
            "removeBase64Images": True,
            "blockAds": True,
            "proxy": "auto",
            "storeInCache": True
        }
    }

    tbs_val = map_search_time_to_tbs_param(search_time)
    if tbs_val:
        payload["tbs"] = tbs_val

    # 调用通用请求执行器发送请求并返回结果
    result = await execute_firecrawl_request(api_url, payload, api_name_key)
    if result is None:
        return compact_error_response(f"{api_name_key}请求失败，接口响应为空。")
    if isinstance(result, dict) and result.get("error"):
        return to_compact_json({
            "success": False,
            "query_details": payload,
            "error": result.get("error"),
            "message": result.get("message", "未知错误"),
            "status_code": result.get("status_code", None),
        })
    if not isinstance(result, dict):
        return compact_error_response(f"{api_name_key}请求失败，接口响应格式异常。")
    if result.get("success") is False:
        return compact_error_response(
            f"{api_name_key}请求失败，上游返回 success=false。",
            extra={"upstream": result}
        )
    if not isinstance(result.get("data"), dict):
        return compact_error_response(
            f"{api_name_key}请求失败，上游返回缺少 data 对象。",
            extra={"upstream": result}
        )
    return to_compact_json(transform_search_result(result))


@mcp.tool(name="firecrawl-web-search")
async def firecrawl_search(
    query: str,
    country: Optional[str] = None,
    search_num: int = 20,
    search_time: Optional[str] = None,
) -> str:
    """
    Web Search Interface
    Parameters:
        query: Search Keywords (Required)
        search_num: Number Of Results To Return, 1~100, Default 20 (Required)
        country: Country Name (Optional)
        search_time: Time Filter, Such As "hour", "day", "week", "month", "year" (Optional)
    Returns:
        Json Format String
    """
    api_name_key = "search"
    api_url = API_ENDPOINTS.get(api_name_key)
    if not api_url:
        return compact_error_response(f"未知API_KEY '{api_name_key}'，无法处理请求。")

    # 解析并规范化国家代码为 ISO2 大写
    country_code = get_country_code_alpha2(country)
    logger.info("search country param '%s' -> resolved country_code: %s", country, country_code)

    # 构建固定的 payload
    payload: Dict[str, Any] = {
        "query": query,
        "limit": validate_search_num(search_num),
        "sources": [{"type": "web"}],
        # tbs 可选：根据传入的 search_time 映射
        "country": country_code,
        "timeout": 60000,
        "ignoreInvalidURLs": False,
        "scrapeOptions": {
            "formats": [],
            "onlyMainContent": True,
            "maxAge": 172800000,
            "waitFor": 0,
            "mobile": False,
            "skipTlsVerification": False,
            "timeout": 30000,
            "parsers": [],
            "location": {
                "country": country_code
            },
            "removeBase64Images": True,
            "blockAds": True,
            "proxy": "auto",
            "storeInCache": True
        }
    }

    tbs_val = map_search_time_to_tbs_param(search_time)
    if tbs_val:
        payload["tbs"] = tbs_val

    # 调用通用请求执行器发送请求并返回结果
    result = await execute_firecrawl_request(api_url, payload, api_name_key)
    if result is None:
        return compact_error_response(f"{api_name_key}请求失败，接口响应为空。")
    if isinstance(result, dict) and result.get("error"):
        return to_compact_json({
            "success": False,
            "query_details": payload,
            "error": result.get("error"),
            "message": result.get("message", "未知错误"),
            "status_code": result.get("status_code", None),
        })
    if not isinstance(result, dict):
        return compact_error_response(f"{api_name_key}请求失败，接口响应格式异常。")
    if result.get("success") is False:
        return compact_error_response(
            f"{api_name_key}请求失败，上游返回 success=false。",
            extra={"upstream": result}
        )
    if not isinstance(result.get("data"), dict):
        return compact_error_response(
            f"{api_name_key}请求失败，上游返回缺少 data 对象。",
            extra={"upstream": result}
        )
    return to_compact_json(transform_search_result(result))


@mcp.tool(name="firecrawl-news-search")
async def firecrawl_search(
    query: str,
    country: Optional[str] = None,
    search_num: int = 20,
    search_time: Optional[str] = None,
) -> str:
    """
    News Search Interface
    Parameters:
        query: Search Keywords (Required)
        search_num: Number Of Results To Return, 1~100, Default 20 (Required)
        country: Country Name (Optional)
        search_time: Time Filter, Such As "hour", "day", "week", "month", "year" (Optional)
    Returns:
        Json Format String
    """
    api_name_key = "search"
    api_url = API_ENDPOINTS.get(api_name_key)
    if not api_url:
        return compact_error_response(f"未知API_KEY '{api_name_key}'，无法处理请求。")

    # 解析并规范化国家代码为 ISO2 大写
    country_code = get_country_code_alpha2(country)
    logger.info("search country param '%s' -> resolved country_code: %s", country, country_code)

    # 构建固定的 payload
    payload: Dict[str, Any] = {
        "query": query,
        "limit": validate_search_num(search_num),
        "sources": [{"type": "news"}],
        # tbs 可选：根据传入的 search_time 映射
        "country": country_code,
        "timeout": 60000,
        "ignoreInvalidURLs": False,
        "scrapeOptions": {
            "formats": [],
            "onlyMainContent": True,
            "maxAge": 172800000,
            "waitFor": 0,
            "mobile": False,
            "skipTlsVerification": False,
            "timeout": 30000,
            "parsers": [],
            "location": {
                "country": country_code
            },
            "removeBase64Images": True,
            "blockAds": True,
            "proxy": "auto",
            "storeInCache": True
        }
    }

    tbs_val = map_search_time_to_tbs_param(search_time)
    if tbs_val:
        payload["tbs"] = tbs_val

    # 调用通用请求执行器发送请求并返回结果
    result = await execute_firecrawl_request(api_url, payload, api_name_key)
    if result is None:
        return compact_error_response(f"{api_name_key}请求失败，接口响应为空。")
    if isinstance(result, dict) and result.get("error"):
        return to_compact_json({
            "success": False,
            "query_details": payload,
            "error": result.get("error"),
            "message": result.get("message", "未知错误"),
            "status_code": result.get("status_code", None),
        })
    if not isinstance(result, dict):
        return compact_error_response(f"{api_name_key}请求失败，接口响应格式异常。")
    if result.get("success") is False:
        return compact_error_response(
            f"{api_name_key}请求失败，上游返回 success=false。",
            extra={"upstream": result}
        )
    if not isinstance(result.get("data"), dict):
        return compact_error_response(
            f"{api_name_key}请求失败，上游返回缺少 data 对象。",
            extra={"upstream": result}
        )
    return to_compact_json(transform_search_result(result))


@mcp.tool(name="firecrawl-image-search")
async def firecrawl_search(
    query: str,
    country: Optional[str] = None,
    search_num: int = 20,
    search_time: Optional[str] = None,
) -> str:
    """
    Image Search Interface
    Parameters:
        query: Search Keywords (Required)
        search_num: Number Of Results To Return, 1~100, Default 20 (Required)
        country: Country Name (Optional)
        search_time: Time Filter, Such As "hour", "day", "week", "month", "year" (Optional)
    Returns:
        Json Format String
    """
    api_name_key = "search"
    api_url = API_ENDPOINTS.get(api_name_key)
    if not api_url:
        return compact_error_response(f"未知API_KEY '{api_name_key}'，无法处理请求。")

    # 解析并规范化国家代码为 ISO2 大写
    country_code = get_country_code_alpha2(country)
    logger.info("search country param '%s' -> resolved country_code: %s", country, country_code)

    # 构建固定的 payload
    payload: Dict[str, Any] = {
        "query": query,
        "limit": validate_search_num(search_num),
        "sources": [{"type": "images"}],
        # tbs 可选：根据传入的 search_time 映射
        "country": country_code,
        "timeout": 60000,
        "ignoreInvalidURLs": False,
        "scrapeOptions": {
            "formats": [],
            "onlyMainContent": True,
            "maxAge": 172800000,
            "waitFor": 0,
            "mobile": False,
            "skipTlsVerification": False,
            "timeout": 30000,
            "parsers": [],
            "location": {
                "country": country_code
            },
            "removeBase64Images": True,
            "blockAds": True,
            "proxy": "auto",
            "storeInCache": True
        }
    }

    tbs_val = map_search_time_to_tbs_param(search_time)
    if tbs_val:
        payload["tbs"] = tbs_val

    # 调用通用请求执行器发送请求并返回结果
    result = await execute_firecrawl_request(api_url, payload, api_name_key)
    if result is None:
        return compact_error_response(f"{api_name_key}请求失败，接口响应为空。")
    if isinstance(result, dict) and result.get("error"):
        return to_compact_json({
            "success": False,
            "query_details": payload,
            "error": result.get("error"),
            "message": result.get("message", "未知错误"),
            "status_code": result.get("status_code", None),
        })
    if not isinstance(result, dict):
        return compact_error_response(f"{api_name_key}请求失败，接口响应格式异常。")
    if result.get("success") is False:
        return compact_error_response(
            f"{api_name_key}请求失败，上游返回 success=false。",
            extra={"upstream": result}
        )
    if not isinstance(result.get("data"), dict):
        return compact_error_response(
            f"{api_name_key}请求失败，上游返回缺少 data 对象。",
            extra={"upstream": result}
        )
    return to_compact_json(transform_search_result(result))


@mcp.tool(name="firecrawl-scrape")
async def firecrawl_scrape(
    url: str,
) -> str:
    """
    网页内容抓取接口。
    参数:
        url: 目标网页URL（必填）
    返回:
        JSON字符串
    """
    api_name_key = "scrape"
    api_url = API_ENDPOINTS.get(api_name_key)
    if not api_url:
        return compact_error_response(f"未知API_KEY '{api_name_key}'，无法处理请求。")

    if not url or not isinstance(url, str) or not url.strip():
        return compact_error_response("参数 url 必填且不能为空字符串。")

    payload: Dict[str, Any] = {
        "url": url,
        "formats": ["markdown"],
        "onlyMainContent": True,
        "includeTags": [],
        "excludeTags": [
            "script",
            "style",
            "noscript",
            "form",
            "input",
            "button",
            "select",
            "textarea",
            "nav",
            ".nav",
            ".navbar",
            ".navigation",
            ".menu",
            ".menubar",
            "header",
            "footer",
            "aside",
            "#comments",
            "#disqus_thread",
            "[class*=\"logo\"]",
            "[class*=\"brand\"]",
            "[id*=\"logo\"]",
            "img[alt*=\"logo\"]",
            "img[src*=\"logo\"]",
            "[id*=\"brand\"]",
            "[class*=\"category--\"]",
            "[id*=\"category--\"]",
            "[class*=\"comment\"]",
            "[id*=\"comment\"]",
            "[class*=\"disqus\"]",
            "[id*=\"disqus\"]",
            "[class^=\"skip\"]",
            "[class*=\"accessib\"]",
            ".sr-only",
            ".visually-hidden",
            "[class*=\"-ad-\"]",
            "[class$=\"-ad\"]",
            "[class^=\"ad-\"]",
            "[class*=\"_ad_\"]",
            "[class$=\"_ad\"]",
            "[class*=\"advert\"]",
            ".sidebar",
            "#sidebar",
            "[class*=\"sidebar\"]",
            "[class*=\"sider\"]",
            "[class^=\"menu-\"]",
            "[class$=\"_module\"]",
            "[class$=\"-module\"]",
            "[class*=\"breadcrumb\"]",
            "[class*=\"pagination\"]",
            "[class*=\"relate\"]",
            "[class*=\"recommend\"]",
            "[class*=\"trending\"]",
            "[class*=\"header\"]",
            "[class*=\"footer\"]",
            "[class$=\"-offset\"]",
            "[class*=\"-offset-\"]"
        ],
        "maxAge": 172800000,
        "headers": {},
        "waitFor": 0,
        "mobile": False,
        "skipTlsVerification": True,
        "timeout": 30000,
        "parsers": ["pdf"],
        "removeBase64Images": True,
        "blockAds": True,
        "proxy": "auto",
        "storeInCache": True,
    }

    result = await execute_firecrawl_request(api_url, payload, api_name_key)
    if result is None:
        return compact_error_response(f"{api_name_key}请求失败，接口响应为空。")
    if isinstance(result, dict) and result.get("error"):
        return to_compact_json({
            "success": False,
            "query_details": payload,
            "error": result.get("error"),
            "message": result.get("message", "未知错误"),
            "status_code": result.get("status_code", None),
        })
    if not isinstance(result, dict):
        return compact_error_response(f"{api_name_key}请求失败，接口响应格式异常。")
    if result.get("success") is False:
        return compact_error_response(
            f"{api_name_key}请求失败，上游返回 success=false。",
            extra={"upstream": result}
        )
    if not isinstance(result.get("data"), dict):
        return compact_error_response(
            f"{api_name_key}请求失败，上游返回缺少 data 对象。",
            extra={"upstream": result}
        )
    return to_compact_json(transform_scrape_result(result))


# 示例：同步阻塞函数，通过线程池异步调用
async def run_blocking_task_in_threadpool(blocking_func, *args, **kwargs):
    loop = asyncio.get_running_loop()
    executor = ThreadPoolManager.get_executor()
    return await loop.run_in_executor(executor, lambda: blocking_func(*args, **kwargs))


async def startup_all():
    global REQUEST_SEMAPHORE
    await AsyncHttpClientManager.startup()
    ThreadPoolManager.startup(max_workers=FIRECRAWL_MAX_WORKERS)
    # 初始化请求并发信号量
    REQUEST_SEMAPHORE = asyncio.Semaphore(FIRECRAWL_MAX_CONCURRENT_REQUESTS)
    logger.info("已初始化请求并发控制，最大并发请求数: %d，线程池最大工作线程: %d", FIRECRAWL_MAX_CONCURRENT_REQUESTS, FIRECRAWL_MAX_WORKERS)
    # 可以扩展这里做更多初始化


async def shutdown_all():
    await AsyncHttpClientManager.shutdown()
    ThreadPoolManager.shutdown()
    # 可以扩展这里做更多清理


def _acquire_process_lock(lock_path: str):
    """
    进程互斥锁：拿不到锁就直接报错退出。
    返回一个可用于释放的对象（文件句柄/FD）。
    """
    # 选择合适的默认锁文件位置（Windows 使用系统临时目录）
    def _default_lock_path() -> str:
        if os.name == "nt":
            return os.path.join(tempfile.gettempdir(), "firecrawl_mcp.lock")
        return "/tmp/firecrawl_mcp.lock"

    lock_path = lock_path or _default_lock_path()

    # Windows 兼容：用 O_EXCL 尝试排他创建
    if os.name == "nt":
        try:
            fd = os.open(lock_path, os.O_CREAT | os.O_EXCL | os.O_RDWR)
            os.write(fd, str(os.getpid()).encode("utf-8"))
            return fd
        except FileExistsError:
            raise RuntimeError(f"无法获取进程锁（{lock_path} 已存在），可能已有实例在运行。")
    else:
        import fcntl  # Unix only
        f = open(lock_path, "a+")
        try:
            fcntl.flock(f.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            f.close()
            raise RuntimeError(f"无法获取进程锁（{lock_path} 被占用），可能已有实例在运行。")
        f.seek(0)
        f.truncate()
        f.write(str(os.getpid()))
        f.flush()
        return f


def _release_process_lock(lock_handle, lock_path: str):
    # 尽量释放并清理锁文件（非强制）
    try:
        if os.name == "nt":
            os.close(lock_handle)
            try:
                os.remove(lock_path)
            except Exception:
                pass
        else:
            import fcntl
            fcntl.flock(lock_handle.fileno(), fcntl.LOCK_UN)
            lock_handle.close()
            try:
                os.remove(lock_path)
            except Exception:
                pass
    except Exception:
        pass


def _env_enabled(name: str, default: bool = False) -> bool:
    v = os.getenv(name)
    if v is None:
        return default
    return str(v).strip().lower() in ("1", "true", "yes", "on")


def main():
    if not API_KEY:
        logger.error("警告：环境变量 FIRECRAWL_API_KEY 未设置，启动后所有接口调用均不可用。")
        logger.error("要快速修复：在项目根目录创建一个名为 .env 的文件，添加一行：FIRECRAWL_API_KEY=\"<your-api-key>\"。")
        logger.error("示例（Linux / macOS）：\n  echo 'FIRECRAWL_API_KEY=\"fc-xxxxxxxxxx\"' > .env")
        logger.error("或者使用 printf：\n  printf 'FIRECRAWL_API_KEY=\"fc-xxxxxxxxxx\"\n' > .env")
        logger.error("完成后重新启动服务。若你已经使用容器或进程管理器，请将该环境变量注入容器/服务配置中。")
    else:
        logger.info("加载到FIRECRAWL_API_KEY，准备启动Firecrawl MCP工具接口服务。")

    # === 互斥启动选择：必须显式开启且只能开启一个 ===
    enable_stdio = _env_enabled("FIRECRAWL_MCP_ENABLE_STDIO", False)
    enable_sse = _env_enabled("FIRECRAWL_MCP_ENABLE_SSE", False)
    enable_http = _env_enabled("FIRECRAWL_MCP_ENABLE_HTTP", False)

    enabled = [("stdio", enable_stdio), ("sse", enable_sse), ("http", enable_http)]
    enabled_names = [name for name, on in enabled if on]

    if len(enabled_names) == 0:
        raise RuntimeError(
            "未选择任何 transport。请显式设置以下环境变量之一为 1/true/on："
            "FIRECRAWL_MCP_ENABLE_STDIO 或 FIRECRAWL_MCP_ENABLE_SSE 或 FIRECRAWL_MCP_ENABLE_HTTP"
        )
    if len(enabled_names) > 1:
        raise RuntimeError(
            f"transport 互斥冲突：同时开启了 {enabled_names}。只能开启一个（stdio/sse/http 三选一）。"
        )

    transport = enabled_names[0]

    default_host = "127.0.0.1"
    default_port = 7001

    if transport == "sse":
        host = os.getenv("FIRECRAWL_MCP_SSE_HOST") or os.getenv("FIRECRAWL_MCP_HOST") or default_host
        port = int(os.getenv("FIRECRAWL_MCP_SSE_PORT") or os.getenv("FIRECRAWL_MCP_PORT") or str(default_port))
    elif transport == "http":
        host = os.getenv("FIRECRAWL_MCP_HTTP_HOST") or os.getenv("FIRECRAWL_MCP_HOST") or default_host
        port = int(os.getenv("FIRECRAWL_MCP_HTTP_PORT") or os.getenv("FIRECRAWL_MCP_PORT") or str(default_port))
    else:
        host = None
        port = None

    # === 进程互斥锁（拿不到锁就报错退出）===
    env_lock = os.getenv("FIRECRAWL_MCP_LOCK_FILE")
    if env_lock:
        lock_path = env_lock
    else:
        # 默认位置：Windows 放到系统临时目录，Unix 放到 /tmp
        if os.name == "nt":
            lock_path = os.path.join(tempfile.gettempdir(), "firecrawl_mcp.lock")
        else:
            lock_path = "/tmp/firecrawl_mcp.lock"
    lock_handle = _acquire_process_lock(lock_path)

    async def _serve():
        await startup_all()
        try:
            if transport == "stdio":
                logger.info("启动 MCP transport=stdio")
                # fastmcp: stdio 默认
                await mcp.run_async()
            else:
                logger.info("启动 MCP transport=%s on %s:%s", transport, host, port)
                await mcp.run_async(transport=transport, host=host, port=port)
        finally:
            logger.info("开始关闭异步资源...")
            await shutdown_all()
            logger.info("Firecrawl MCP工具接口服务已安全关闭。")

    try:
        asyncio.run(_serve())
    except KeyboardInterrupt:
        logger.info("接收到中断信号，正在关闭服务...")
    finally:
        _release_process_lock(lock_handle, lock_path)


# This block is for direct execution via 'python -m firecrawl_toolkit.server'
if __name__ == "__main__":
    main()
