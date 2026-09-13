#!/usr/bin/env python3
"""OpenAI-compatible prompt guard adapter for Alibaba Cloud MultiModalGuard.

The Sub2API prompt-audit module only knows how to call a Qwen3Guard-like
OpenAI chat-completions endpoint. This adapter keeps Alibaba credentials on the
adapter service and exposes only the tiny contract Sub2API needs:

- GET  /v1/models
- POST /v1/chat/completions

It never logs prompt content or credentials. Logs contain only request IDs,
Alibaba request IDs, and high-level decisions for auditability.
"""

from __future__ import annotations

import json
import os
import re
import sys
import time
import uuid
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

from alibabacloud_green20220302 import models
from alibabacloud_green20220302.client import Client
from alibabacloud_tea_openapi.models import Config
from alibabacloud_tea_util.models import RuntimeOptions


MODEL_ID = os.getenv("ADAPTER_MODEL", "aliyun-multimodalguard-qwen3guard")
ALIYUN_ENDPOINT = os.getenv("ALIYUN_ENDPOINT", "green-cip.eu-central-1.aliyuncs.com")
ALIYUN_SERVICE = os.getenv("ALIYUN_SERVICE", "query_security_check_cb")
CONNECT_TIMEOUT_MS = int(os.getenv("ALIYUN_CONNECT_TIMEOUT_MS", "3000"))
READ_TIMEOUT_MS = int(os.getenv("ALIYUN_READ_TIMEOUT_MS", "10000"))
MAX_BODY_BYTES = int(os.getenv("MAX_BODY_BYTES", str(1024 * 1024)))
MAX_PROMPT_CHARS = int(os.getenv("MAX_PROMPT_CHARS", "100000"))
ALIYUN_MAX_CONTENT_CHARS = int(os.getenv("ALIYUN_MAX_CONTENT_CHARS", "1800"))
ADAPTER_API_KEY = os.getenv("ADAPTER_API_KEY", "").strip()
FAIL_OPEN_ON_GUARDRAIL_UNAVAILABLE = os.getenv("FAIL_OPEN_ON_GUARDRAIL_UNAVAILABLE", "true").strip().lower() in {
    "1",
    "true",
    "yes",
    "on",
}
LOCAL_FALLBACK_ENABLED = os.getenv("LOCAL_FALLBACK_ENABLED", "true").strip().lower() in {
    "1",
    "true",
    "yes",
    "on",
}
LOCAL_FALLBACK_DEFAULT_SAFETY = os.getenv("LOCAL_FALLBACK_DEFAULT_SAFETY", "Safe").strip() or "Safe"
SIMULATE_GUARDRAIL_UNAVAILABLE = os.getenv("SIMULATE_GUARDRAIL_UNAVAILABLE", "").strip().lower() in {
    "1",
    "true",
    "yes",
    "on",
}


QWEN3_CATEGORIES = {
    "violent",
    "non_violent_illegal_acts",
    "sexual_content_or_sexual_acts",
    "pii",
    "suicide_and_self_harm",
    "unethical_acts",
    "politically_sensitive_topics",
    "copyright_violation",
    "jailbreak",
}


LOCAL_HIGH_RISK_RULES: tuple[tuple[str, str, re.Pattern[str]], ...] = (
    (
        "jailbreak",
        "jailbreak",
        re.compile(
            r"(?is)(ignore|bypass|override|disable).{0,80}"
            r"(system|developer|safety|policy|guardrail|moderation|filter|instruction)"
            r"|越狱|绕过.{0,20}(审核|安全|限制|规则)|忽略.{0,20}(系统|开发者|安全|规则|限制)",
        ),
    ),
    (
        "credential_theft",
        "non_violent_illegal_acts",
        re.compile(
            r"(?is)(steal|dump|exfiltrate|extract|harvest).{0,80}"
            r"(password|credential|cookie|token|api[_ -]?key|session|oauth|jwt)"
            r"|盗取.{0,20}(密码|凭证|cookie|token|密钥|会话)"
            r"|窃取.{0,20}(密码|凭证|cookie|token|密钥|会话)",
        ),
    ),
    (
        "malware",
        "non_violent_illegal_acts",
        re.compile(
            r"(?is)(ransomware|keylogger|credential stealer|reverse shell|botnet|rootkit)"
            r"|恶意软件|勒索软件|键盘记录器|远控木马|反弹shell|后门程序",
        ),
    ),
    (
        "violent_weapon",
        "violent",
        re.compile(
            r"(?is)(build|make|manufacture|assemble).{0,80}"
            r"(bomb|explosive|grenade|unregistered firearm|ghost gun)"
            r"|制作.{0,20}(炸弹|爆炸物|枪支|手枪|步枪)"
            r"|自制.{0,20}(炸弹|爆炸物|枪支|手枪|步枪)",
        ),
    ),
    (
        "sexual_minors",
        "sexual_content_or_sexual_acts",
        re.compile(
            r"(?is)(child sexual|minor sexual|underage sexual|csam)"
            r"|未成年.{0,20}(性|色情|裸照|性行为)"
            r"|儿童.{0,20}(性|色情|裸照|性行为)",
        ),
    ),
    (
        "self_harm",
        "suicide_and_self_harm",
        re.compile(
            r"(?is)(how to|best way to|instructions? to).{0,80}"
            r"(kill myself|commit suicide|self harm)"
            r"|如何.{0,20}(自杀|自残)|怎么.{0,20}(自杀|自残)",
        ),
    ),
    (
        "fraud",
        "non_violent_illegal_acts",
        re.compile(
            r"(?is)(phishing kit|carding|bypass kyc|fake id|money laundering)"
            r"|钓鱼网站|洗钱|伪造.{0,20}(身份证|护照|证件)|绕过.{0,20}实名",
        ),
    ),
)


def build_aliyun_client() -> Client:
    access_key_id = os.getenv("ALIYUN_ACCESS_KEY_ID", "").strip()
    access_key_secret = os.getenv("ALIYUN_ACCESS_KEY_SECRET", "").strip()
    if not access_key_id or not access_key_secret:
        raise RuntimeError("ALIYUN_ACCESS_KEY_ID and ALIYUN_ACCESS_KEY_SECRET are required")
    return Client(
        Config(
            access_key_id=access_key_id,
            access_key_secret=access_key_secret,
            endpoint=ALIYUN_ENDPOINT,
        )
    )


ALIYUN_CLIENT = build_aliyun_client()


def write_log(event: str, **fields: Any) -> None:
    safe_fields = {"event": event, **fields}
    print(json.dumps(safe_fields, ensure_ascii=False, separators=(",", ":")), flush=True)


def extract_text_part(content: Any) -> str:
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts: list[str] = []
        for item in content:
            if not isinstance(item, dict):
                continue
            if item.get("type") == "text" and isinstance(item.get("text"), str):
                parts.append(item["text"])
            elif isinstance(item.get("text"), str):
                parts.append(item["text"])
        return "\n".join(part for part in parts if part.strip())
    return ""


def extract_prompt(payload: dict[str, Any]) -> str:
    messages = payload.get("messages")
    if not isinstance(messages, list):
        return ""

    # Sub2API currently sends one user message, but accepting the whole textual
    # conversation makes the adapter tolerant to future probe/runtime changes.
    parts: list[str] = []
    for message in messages:
        if not isinstance(message, dict):
            continue
        text = extract_text_part(message.get("content"))
        if text.strip():
            parts.append(text)
    prompt = "\n\n".join(parts).strip()
    if len(prompt) > MAX_PROMPT_CHARS:
        prompt = prompt[:MAX_PROMPT_CHARS]
    return prompt


def normalize_suggestion(value: Any) -> str:
    suggestion = str(value or "").strip().lower()
    if suggestion == "pass":
        return "pass"
    if suggestion == "block":
        return "block"
    if suggestion in {"review", "watch", "mask"}:
        return "review"
    return "review"


def safety_from_suggestion(suggestion: str) -> str:
    if suggestion == "pass":
        return "Safe"
    if suggestion == "review":
        return "Controversial"
    return "Unsafe"


def category_from_detail(detail: dict[str, Any]) -> str | None:
    detail_type = str(detail.get("type") or "").lower()
    joined = " ".join(
        str(value)
        for value in [
            detail.get("type"),
            detail.get("label"),
            detail.get("description"),
            *[
                item.get("label", "")
                for item in detail.get("result", [])
                if isinstance(item, dict)
            ],
            *[
                item.get("description", "")
                for item in detail.get("result", [])
                if isinstance(item, dict)
            ],
        ]
    ).lower()

    if "promptattack" in detail_type or "attack" in joined or "jailbreak" in joined or "越狱" in joined:
        return "jailbreak"
    if "sensitivedata" in detail_type or "pii" in joined or "personal" in joined or "privacy" in joined or "隐私" in joined or "个人" in joined:
        return "pii"
    if "sexual" in joined or "色情" in joined or "性" in joined:
        return "sexual_content_or_sexual_acts"
    if "suicide" in joined or "self-harm" in joined or "自杀" in joined or "自残" in joined:
        return "suicide_and_self_harm"
    if "violence" in joined or "violent" in joined or "暴力" in joined:
        return "violent"
    if "politic" in joined or "政治" in joined:
        return "politically_sensitive_topics"
    if "copyright" in joined or "版权" in joined:
        return "copyright_violation"
    if "illegal" in joined or "crime" in joined or "违法" in joined or "犯罪" in joined:
        return "non_violent_illegal_acts"
    if "contentmoderation" in detail_type:
        return "unethical_acts"
    return None


def categories_from_response(data: Any) -> list[str]:
    if not data:
        return []
    details = getattr(data, "detail", None)
    if not details:
        return []

    categories: list[str] = []
    for detail in details:
        detail_map = detail.to_map() if hasattr(detail, "to_map") else detail
        if not isinstance(detail_map, dict):
            continue
        if normalize_suggestion(detail_map.get("suggestion")) == "pass":
            continue
        category = category_from_detail(detail_map)
        if category and category in QWEN3_CATEGORIES and category not in categories:
            categories.append(category)
    if not categories:
        categories.append("unethical_acts")
    return categories


def normalize_default_fallback_safety() -> str:
    candidate = LOCAL_FALLBACK_DEFAULT_SAFETY.strip().lower()
    if candidate == "controversial":
        return "Controversial"
    if candidate == "unsafe":
        return "Unsafe"
    return "Safe"


def local_fallback_guard(prompt: str) -> tuple[str, list[str], list[str]]:
    """Classify obvious high-risk prompts without calling Alibaba Cloud.

    This is intentionally conservative. It only blocks high-confidence patterns
    during upstream guardrail outages, so a billing or network issue does not
    take down the main request path while still catching the riskiest abuse.
    """

    if not LOCAL_FALLBACK_ENABLED:
        return normalize_default_fallback_safety(), [], []

    categories: list[str] = []
    rules: list[str] = []
    for rule_id, category, pattern in LOCAL_HIGH_RISK_RULES:
        if not pattern.search(prompt):
            continue
        rules.append(rule_id)
        if category not in categories:
            categories.append(category)

    if categories:
        return "Unsafe", categories, rules
    return normalize_default_fallback_safety(), [], []


def call_guardrail_once(prompt: str, request_id: str) -> tuple[str, list[str], str]:
    if SIMULATE_GUARDRAIL_UNAVAILABLE:
        raise RuntimeError("simulated_guardrail_unavailable")
    service_parameters = json.dumps(
        {
            "content": prompt,
            "dataId": f"cn-junfeiai-{request_id}",
        },
        ensure_ascii=False,
    )
    request = models.MultiModalGuardRequest(
        service=ALIYUN_SERVICE,
        service_parameters=service_parameters,
    )
    response = ALIYUN_CLIENT.multi_modal_guard_with_options(
        request,
        RuntimeOptions(read_timeout=READ_TIMEOUT_MS, connect_timeout=CONNECT_TIMEOUT_MS),
    )
    body = response.body
    code = getattr(body, "code", None)
    if code != 200:
        message = str(getattr(body, "message", "") or "").strip()
        aliyun_request_id = str(getattr(body, "request_id", "") or "").strip()
        detail = f"aliyun_guardrail_code_{code}"
        if message:
            detail += f": {message}"
        if aliyun_request_id:
            detail += f" request_id={aliyun_request_id}"
        raise RuntimeError(detail)

    data = getattr(body, "data", None)
    suggestion = normalize_suggestion(getattr(data, "suggestion", None))
    categories = categories_from_response(data)
    if suggestion == "pass":
        categories = []
    return safety_from_suggestion(suggestion), categories, str(getattr(body, "request_id", "") or "")


def prompt_chunks(prompt: str) -> list[str]:
    max_chars = max(128, min(ALIYUN_MAX_CONTENT_CHARS, MAX_PROMPT_CHARS))
    return [prompt[i : i + max_chars] for i in range(0, len(prompt), max_chars)] or [prompt]


def call_guardrail(prompt: str, request_id: str) -> tuple[str, list[str], str]:
    highest = "pass"
    categories: list[str] = []
    request_ids: list[str] = []
    for index, chunk in enumerate(prompt_chunks(prompt), start=1):
        safety, chunk_categories, aliyun_request_id = call_guardrail_once(chunk, f"{request_id}-{index}")
        if aliyun_request_id:
            request_ids.append(aliyun_request_id)
        if safety == "Unsafe":
            highest = "block"
        elif safety == "Controversial" and highest != "block":
            highest = "review"
        for category in chunk_categories:
            if category not in categories:
                categories.append(category)
    if highest == "pass":
        categories = []
    return safety_from_suggestion(highest), categories, ",".join(request_ids)


def openai_completion(model: str, content: str) -> dict[str, Any]:
    now = int(time.time())
    return {
        "id": f"chatcmpl-guard-{uuid.uuid4().hex[:24]}",
        "object": "chat.completion",
        "created": now,
        "model": model or MODEL_ID,
        "choices": [
            {
                "index": 0,
                "message": {"role": "assistant", "content": content},
                "finish_reason": "stop",
            }
        ],
        "usage": {"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
    }


class Handler(BaseHTTPRequestHandler):
    server_version = "aliyun-guardrail-openai-adapter/1.0"

    def log_message(self, fmt: str, *args: Any) -> None:
        # Keep access logs structured and content-free.
        write_log("http_access", method=self.command, path=self.path, status=args[1] if len(args) > 1 else "")

    def require_auth(self) -> bool:
        if not ADAPTER_API_KEY:
            return True
        header = self.headers.get("Authorization", "")
        expected = f"Bearer {ADAPTER_API_KEY}"
        if header == expected:
            return True
        self.write_json(HTTPStatus.UNAUTHORIZED, {"error": {"message": "unauthorized", "type": "authentication_error"}})
        return False

    def read_json(self) -> dict[str, Any] | None:
        content_length = int(self.headers.get("Content-Length") or "0")
        if content_length <= 0 or content_length > MAX_BODY_BYTES:
            self.write_json(HTTPStatus.BAD_REQUEST, {"error": {"message": "invalid request body", "type": "invalid_request_error"}})
            return None
        raw = self.rfile.read(content_length)
        try:
            payload = json.loads(raw)
        except json.JSONDecodeError:
            self.write_json(HTTPStatus.BAD_REQUEST, {"error": {"message": "invalid json", "type": "invalid_request_error"}})
            return None
        if not isinstance(payload, dict):
            self.write_json(HTTPStatus.BAD_REQUEST, {"error": {"message": "invalid json object", "type": "invalid_request_error"}})
            return None
        return payload

    def write_json(self, status: HTTPStatus, payload: dict[str, Any]) -> None:
        body = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self) -> None:
        if self.path.rstrip("/") == "/healthz":
            self.write_json(HTTPStatus.OK, {"ok": True, "model": MODEL_ID, "endpoint": ALIYUN_ENDPOINT, "service": ALIYUN_SERVICE})
            return
        if self.path.rstrip("/") == "/v1/models":
            if not self.require_auth():
                return
            self.write_json(
                HTTPStatus.OK,
                {
                    "object": "list",
                    "data": [
                        {
                            "id": MODEL_ID,
                            "object": "model",
                            "created": 0,
                            "owned_by": "junfeiai",
                        }
                    ],
                },
            )
            return
        self.write_json(HTTPStatus.NOT_FOUND, {"error": {"message": "not found", "type": "invalid_request_error"}})

    def do_POST(self) -> None:
        if self.path.rstrip("/") != "/v1/chat/completions":
            self.write_json(HTTPStatus.NOT_FOUND, {"error": {"message": "not found", "type": "invalid_request_error"}})
            return
        if not self.require_auth():
            return
        request_id = self.headers.get("X-Request-Id") or uuid.uuid4().hex
        payload = self.read_json()
        if payload is None:
            return
        model = str(payload.get("model") or MODEL_ID)
        prompt = extract_prompt(payload)
        if not prompt:
            self.write_json(HTTPStatus.BAD_REQUEST, {"error": {"message": "empty prompt", "type": "invalid_request_error"}})
            return

        started = time.time()
        try:
            safety, categories, aliyun_request_id = call_guardrail(prompt, request_id)
        except Exception as exc:  # noqa: BLE001 - keep the main gateway available without prompt leakage.
            fallback_safety, fallback_categories, fallback_rules = local_fallback_guard(prompt)
            write_log(
                "guardrail_degraded",
                request_id=request_id,
                error=str(exc),
                fallback_safety=fallback_safety,
                fallback_categories=fallback_categories,
                fallback_rules=fallback_rules,
                fail_open=FAIL_OPEN_ON_GUARDRAIL_UNAVAILABLE,
                latency_ms=int((time.time() - started) * 1000),
            )
            if FAIL_OPEN_ON_GUARDRAIL_UNAVAILABLE:
                safety, categories, aliyun_request_id = fallback_safety, fallback_categories, ""
            else:
                self.write_json(HTTPStatus.SERVICE_UNAVAILABLE, {"error": {"message": "guardrail unavailable", "type": "server_error"}})
                return

        category_text = "None" if not categories else ", ".join(categories)
        guard_content = f"Safety: {safety}\nCategories: {category_text}"
        write_log(
            "guardrail_done",
            request_id=request_id,
            aliyun_request_id=aliyun_request_id,
            safety=safety,
            categories=categories,
            latency_ms=int((time.time() - started) * 1000),
        )
        self.write_json(HTTPStatus.OK, openai_completion(model, guard_content))


def main() -> int:
    host = os.getenv("HOST", "0.0.0.0")
    port = int(os.getenv("PORT", "8080"))
    httpd = ThreadingHTTPServer((host, port), Handler)
    write_log("adapter_started", host=host, port=port, model=MODEL_ID, endpoint=ALIYUN_ENDPOINT, service=ALIYUN_SERVICE)
    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        write_log("adapter_stopped")
        return 0
    return 0


if __name__ == "__main__":
    sys.exit(main())
