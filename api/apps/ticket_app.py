#
#  Copyright 2024 The InfiniFlow Authors. All Rights Reserved.
#
#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.
#
import logging
import os
from quart import request

from api.apps import current_user, login_required
from api.utils.api_utils import get_json_result, server_error_response

ZAMMAD_URL = os.getenv("ZAMMAD_URL", "http://zammad:8080/api/v1/")
ZAMMAD_TOKEN = os.getenv("ZAMMAD_TOKEN", "")

_zammad_client = None


def _get_zammad_client():
    global _zammad_client
    if _zammad_client is None:
        from zammad_py import ZammadAPI

        _zammad_client = ZammadAPI(url=ZAMMAD_URL, http_token=ZAMMAD_TOKEN)
    return _zammad_client


def _ensure_zammad_customer(client, email, nickname):
    """Look up or create a Zammad customer user by email. Returns user id or None."""
    if not email or "@" not in email:
        return None

    try:
        existing = list(client.user.search(f'email:"{email}"'))
        if existing:
            customer_user = existing[0]
        else:
            firstname = nickname or email.split("@")[0]
            lastname = ""
            if firstname and " " in firstname:
                parts = firstname.split(maxsplit=1)
                firstname = parts[0]
                lastname = parts[1] if len(parts) > 1 else ""
            customer_user = client.user.create(
                params={
                    "email": email,
                    "firstname": firstname,
                    "lastname": lastname,
                    "role_ids": [3],
                }
            )
        return customer_user.get("id") if isinstance(customer_user, dict) else getattr(customer_user, "id", None)
    except Exception as user_err:
        logging.warning("Skip ensuring zammad customer %s: %s", email, user_err)
        return None


def _display_name(email, nickname):
    """Return a display name for the given email and nickname."""
    name = (nickname or "").strip()
    if name:
        return name
    if email and "@" in email:
        return email.split("@")[0]
    return email


_LUCENE_SPECIAL_CHARS = r'\+-=&|><!(){}[]^"~*?:/'


def _escape_lucene(value):
    return "".join("\\" + ch if ch in _LUCENE_SPECIAL_CHARS else ch for ch in value)


def _build_title_match_clause(keyword):
    """Build a Zammad/Elasticsearch query clause that matches the keyword
    against the ticket title as a case-insensitive substring search.

    Multi-word keywords are split into tokens and AND-combined so each
    token must appear somewhere in the title.
    """
    tokens = [tok for tok in keyword.split() if tok]
    if not tokens:
        return ""
    clauses = [f"title:*{_escape_lucene(tok)}*" for tok in tokens]
    if len(clauses) == 1:
        return clauses[0]
    return "(" + " AND ".join(clauses) + ")"


@manager.route("/groups", methods=["GET"])  # noqa: F821
@login_required
async def list_groups():
    try:
        client = _get_zammad_client()
        groups = list(client.group.all())
        return get_json_result(data=groups)
    except Exception as e:
        logging.exception("Failed to list zammad groups")
        return server_error_response(e)


@manager.route("/", methods=["GET"])  # noqa: F821
@login_required
async def list_tickets():
    try:
        client = _get_zammad_client()
        page = int(request.args.get("page", 1))
        page_size = int(request.args.get("page_size", 20))
        keywords = (request.args.get("keywords") or "").strip()

        email = (getattr(current_user, "email", "") or "").strip()
        if not email:
            return get_json_result(data={"list": [], "total": 0})

        # Use Zammad's DB-backed /tickets index (not /tickets/search) so a
        # just-created ticket shows up without waiting for the ~1-2s
        # Elasticsearch refresh. X-On-Behalf-Of scopes results to the
        # current user, bypassing the master token's admin perspective.
        ticket_list = []
        try:
            list_resp = client.ticket._connection.session.get(
                client.ticket.url,
                params={
                    "page": page,
                    "per_page": page_size,
                    "expand": "true",
                    "sort_by": "created_at",
                    "order_by": "desc",
                },
                headers={"X-On-Behalf-Of": email},
            )
            list_resp.raise_for_status()
            payload = list_resp.json() or []
            ticket_list = payload if isinstance(payload, list) else (payload.get("tickets") or payload.get("list") or [])
        except Exception as list_err:
            logging.warning("Failed to fetch zammad tickets via on_behalf_of: %s", list_err)
            ticket_list = []

        # Best-effort: enforce newest-first within the page even if Zammad
        # ignored sort_by/order_by on the index endpoint.
        ticket_list.sort(key=lambda t: t.get("created_at") or "", reverse=True)

        # Index endpoint doesn't text-filter, so apply title keyword on
        # the current page (case-insensitive substring).
        if keywords:
            kw_lower = keywords.lower()
            ticket_list = [t for t in ticket_list if kw_lower in (t.get("title") or "").lower()]

        # Index endpoint doesn't return a total count; pull it from /search.
        # The count may lag the list by 1~2s while ES catches up, but the
        # list above is already accurate so the user still sees the new ticket.
        total = len(ticket_list) + (page - 1) * page_size
        try:
            query = f'customer.email:"{email}"'
            if keywords:
                title_clause = _build_title_match_clause(keywords)
                if title_clause:
                    query = f"{query} AND {title_clause}"
            count_resp = client.ticket._connection.session.get(
                f"{client.ticket.url}/search",
                params={"query": query, "per_page": 1, "full": "true"},
            )
            count_resp.raise_for_status()
            count_data = count_resp.json() or {}
            total = max(total, int(count_data.get("total_count", 0)))
        except Exception as count_err:
            logging.warning("Failed to fetch zammad ticket total count: %s", count_err)

        return get_json_result(data={"total": total, "list": ticket_list})
    except Exception as e:
        logging.exception("Failed to list zammad tickets")
        return server_error_response(e)


@manager.route("/", methods=["POST"])  # noqa: F821
@login_required
async def create_ticket():
    try:
        from quart import request as quart_request

        req = await quart_request.get_json() or {}
        client = _get_zammad_client()

        email = (getattr(current_user, "email", "") or "").strip()
        nickname = (getattr(current_user, "nickname", "") or "").strip()
        display_name = _display_name(email, nickname)

        customer_id = _ensure_zammad_customer(client, email, display_name)

        if email:
            req["customer"] = email
        if customer_id:
            req["customer_id"] = customer_id

        article = req.get("article") or {}
        article["type"] = article.get("type") or "web"
        article["sender"] = "Customer"
        if display_name and email:
            article["from"] = f"{display_name} <{email}>"
        elif email:
            article["from"] = email
        if customer_id:
            article["origin_by_id"] = customer_id
        req["article"] = article

        # Attachments are passed directly from frontend in Zammad format
        # { filename, data: base64, mime-type }
        attachments = req.get("attachments")
        if attachments:
            article["attachments"] = attachments

        new_ticket = client.ticket.create(params=req)
        return get_json_result(data=new_ticket)
    except Exception as e:
        logging.exception("Failed to create zammad ticket")
        return server_error_response(e)


@manager.route("/<int:ticket_id>", methods=["GET"])  # noqa: F821
@login_required
async def get_ticket(ticket_id: int):
    try:
        client = _get_zammad_client()
        response = client.ticket._connection.session.get(
            f"{client.ticket.url}/{ticket_id}",
            params={"expand": "true"},
        )
        response.raise_for_status()
        ticket = response.json()
        return get_json_result(data=ticket)
    except Exception as e:
        logging.exception("Failed to get zammad ticket")
        return server_error_response(e)


@manager.route("/<int:ticket_id>/articles", methods=["GET"])  # noqa: F821
@login_required
async def get_ticket_articles(ticket_id: int):
    try:
        client = _get_zammad_client()
        articles = client.ticket.articles(id=ticket_id)
        return get_json_result(data=articles)
    except Exception as e:
        logging.exception("Failed to get zammad ticket articles")
        return server_error_response(e)


@manager.route("/<int:ticket_id>/close", methods=["POST"])  # noqa: F821
@login_required
async def close_ticket(ticket_id: int):
    try:
        client = _get_zammad_client()
        updated = client.ticket.update(id=ticket_id, params={"state": "closed"})
        return get_json_result(data=updated)
    except Exception as e:
        logging.exception("Failed to close zammad ticket")
        return server_error_response(e)


@manager.route("/<int:ticket_id>/articles", methods=["POST"])  # noqa: F821
@login_required
async def reply_ticket(ticket_id: int):
    try:
        req = await request.get_json() or {}
        body = (req.get("body") or "").strip()
        if not body:
            return server_error_response(ValueError("body is required"))

        client = _get_zammad_client()

        email = (getattr(current_user, "email", "") or "").strip()
        nickname = (getattr(current_user, "nickname", "") or "").strip()
        display_name = _display_name(email, nickname)
        customer_id = _ensure_zammad_customer(client, email, display_name)

        params = {
            "ticket_id": ticket_id,
            "body": body,
            "type": req.get("type") or "web",
            "internal": bool(req.get("internal", False)),
            "sender": "Customer",
            "content_type": req.get("content_type") or "text/plain",
        }
        if req.get("subject"):
            params["subject"] = req["subject"]
        if display_name and email:
            params["from"] = f"{display_name} <{email}>"
        elif email:
            params["from"] = email
        if customer_id:
            params["origin_by_id"] = customer_id

        attachments = req.get("attachments")
        if attachments:
            params["attachments"] = attachments

        article = client.ticket_article.create(params=params)
        return get_json_result(data=article)
    except Exception as e:
        logging.exception("Failed to reply zammad ticket")
        return server_error_response(e)


@manager.route("/<int:ticket_id>/articles/<int:article_id>/attachments/<int:attachment_id>", methods=["GET"])  # noqa: F821
@login_required
async def download_attachment(ticket_id: int, article_id: int, attachment_id: int):
    try:
        client = _get_zammad_client()
        response = client.ticket_article_attachment._connection.session.get(f"{client.ticket_article_attachment.url}/{ticket_id}/{article_id}/{attachment_id}")
        response.raise_for_status()

        from quart import Response

        return Response(
            response=response.content,
            status=response.status_code,
            headers={
                "Content-Type": response.headers.get("Content-Type", "application/octet-stream"),
                "Content-Disposition": response.headers.get("Content-Disposition", f"attachment; filename=attachment-{attachment_id}"),
            },
        )
    except Exception as e:
        logging.exception("Failed to download zammad attachment")
        return server_error_response(e)
