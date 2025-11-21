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
from quart import request, jsonify
from api.apps import login_required, current_user
from common import settings
from common.constants import RetCode
from api.utils.api_utils import get_json_result, server_error_response
from api.db.services.billing_service import TenantPlanService
from api.db.services.user_service import UserService
import stripe
import json

import logging


@manager.route("/checkout", methods=["POST"])  # noqa: F821
@login_required
async def billing_checkout():
    """
    https://docs.stripe.com/payments/accept-a-payment
    """
    req = await request.json
    tenant_id = req.get("tenant_id")
    price_id = req.get("price_id")
    if not tenant_id or not price_id:
        return get_json_result(
            data=False,
            message="Missing required parameters tenant_id and price_id.",
            code=RetCode.PARAMETER_ERROR,
        )
    if current_user.id != tenant_id:
        return get_json_result(
            data=False,
            message="No authorization.",
            code=RetCode.AUTHENTICATION_ERROR,
        )
    try:
        tenant_plan = TenantPlanService.get_by_tenant_id(tenant_id)
        subscription_id = tenant_plan.get("subscription_id")
        subscription_status = tenant_plan.get("subscription_status")
        # Stripe has built-in retry logic and will automatically retry deductions after a deduction failure. During the retry period, the subscription status may still be active and will only change to past_due after all retry attempts fail.
        if subscription_status == "active":
            # https://docs.stripe.com/api/subscriptions/update
            subscription = stripe.Subscription.retrieve(subscription_id)
            subscription_items = subscription['items']['data']
            items = []
            exists = False
            for item in subscription_items:
                if item['price']['id'] == price_id:
                    exists = True
                    break
                else:
                    items.append({
                        'id': item['id'],
                        'deleted': True,  # delete the existing item
                    })
            if not exists:
                items.append({
                    'price': price_id,
                    "quantity": 1,
                })
            elif 1==len(subscription_items):
                msg = f"Tenant {tenant_id} already has an active subscription {subscription_id} on price {price_id}"
                return get_json_result(data=False, message=msg, code=RetCode.SUCCESS)
            stripe.Subscription.modify(
                subscription_id,
                items=items,
                proration_behavior='always_invoice',  # charge for an upgrade immediately
            )
            msg = f"Tenant {tenant_id} subscription {subscription_id} has been updated to price {price_id}. Stripe.com will immediately generate an invoice, calculate the price difference, and adjust the customer's bill according to the price difference."
            logging.info(msg)
            return get_json_result(data=False, message=msg, code=RetCode.SUCCESS)
        if subscription_id:
            stripe.Subscription.delete(subscription_id, cancellation_details={"comment": "checkout", "feedback": "other"}, prorate=True)
            msg = f"Tenant {tenant_id} subscription {subscription_id} has been cancelled to ensure one customer has no more than one subscription."
            logging.info(msg)
        customer_id = tenant_plan.get("customer_id")
        if not customer_id:
            user = UserService.filter_by_id(tenant_id)
            customer = stripe.Customer.create(
                email=user.email, metadata={"tenant_id": tenant_id}
            )
            customer_id = customer.id
            TenantPlanService.set_customer_id(tenant_id, customer_id)
            logging.info(
                f"created customer {customer_id} for tenant {tenant_id}, email {user.email}"
            )
        else:
            logging.info(f"found customer {customer_id} for tenant {tenant_id}")
        session = stripe.checkout.Session.create(
            customer=customer_id,
            line_items=[
                {
                    # Provide the exact Price ID (e.g. pr_1234) of the product you want to sell
                    "price": price_id,
                    "quantity": 1,
                }
            ],
            mode="subscription",
            success_url=settings.BILLING["session_success_url"],
            cancel_url=settings.BILLING["session_cancel_url"],
        )
        logging.info(f"created stripe session id {session.id}, url: {session.url}")
        return get_json_result(data={"redirect_to": session.url})

    except Exception as e:
        return server_error_response(e)

@manager.route("/unsubscribe", methods=["POST"])  # noqa: F821
@login_required
async def billing_unsubscribe():
    req = await request.json
    tenant_id = req.get("tenant_id")
    # https://docs.stripe.com/api/subscriptions/cancel
    # Possible enum values of feedback: customer_service, low_quality, missing_features, other, switched_service, too_complex, too_expensive, unused
    feedback = req.get("feedback")
    comment = req.get("comment")
    cancel_at_period_end = req.get("cancel_at_period_end")
    if not tenant_id:
        return get_json_result(
            data=False,
            message="Missing required parameters tenant_id and price_id.",
            code=RetCode.PARAMETER_ERROR,
        )
    if current_user.id != tenant_id:
        return get_json_result(
            data=False,
            message="No authorization.",
            code=RetCode.AUTHENTICATION_ERROR,
        )
    try:
        tenant_plan = TenantPlanService.get_by_tenant_id(tenant_id)
        subscription_id = tenant_plan.get("subscription_id")
        if not subscription_id:
            msg = f"Tenant {tenant_id} has no subscription."
            logging.info(msg)
            return get_json_result(data=False, message=msg, code=RetCode.SUCCESS)
        if cancel_at_period_end == 'yes':
            _ = stripe.Subscription.modify(subscription_id, cancel_at_period_end=True)
            msg = f"Tenant {tenant_id} subscription {subscription_id} will be cancelled at the end of the current period."
        else:
            _ = stripe.Subscription.delete(subscription_id, cancellation_details={"comment": comment, "feedback": feedback}, prorate=True)
            msg = f"Tenant {tenant_id} subscription {subscription_id} has been cancelled."
        return get_json_result(data=False, message=msg, code=RetCode.SUCCESS)
    except Exception as e:
        return server_error_response(e)


@manager.route("/webhook", methods=["POST"])
async def billing_webhook():
    """
    https://docs.stripe.com/webhooks/quickstart
    """
    event = None
    payload = await request.data

    try:
        event = json.loads(payload)
    except json.decoder.JSONDecodeError:
        logging.exception("billing_webhook error while parsing basic request.")
        return jsonify(success=False)
    if settings.BILLING["stripe_endpoint_secret"]:
        # Only verify the event if there is an endpoint secret defined
        # Otherwise use the basic event deserialized with json
        sig_header = request.headers.get("stripe-signature")
        try:
            event = stripe.Webhook.construct_event(
                payload, sig_header, settings.BILLING["stripe_endpoint_secret"]
            )
        except stripe.error.SignatureVerificationError:
            logging.exception("billing_webhook signature verification failed.")
            return jsonify(success=False)

    # Handle the event
    SUBSCRIPTION_UPDATED = 'customer.subscription.updated'
    SUBSCRIPTION_DELETED = 'customer.subscription.deleted'
    event_type = event["type"]
    if event_type in [SUBSCRIPTION_UPDATED, SUBSCRIPTION_DELETED]:
        logging.debug(f"billing_webhook got event: {event}")
        subscription = event["data"]["object"]
        # Refers to https://docs.stripe.com/api/subscriptions/object
        subscription_id = subscription["id"]
        subscription_status = subscription["status"]
        customer_id = subscription["customer"]
        price_id = subscription["items"]["data"][0]["price"]["id"]
        plan_name = settings.PRICE_PLAN.get(price_id)
        if not plan_name:
            msg = f"billing_webhook could not find plan for price {price_id}"
            logging.warning(msg)
            return jsonify(success=False)
        updated_rows = TenantPlanService.update_subscription(
            customer_id, subscription_id, subscription_status, plan_name
        )
        if not updated_rows:
            msg = f"billing_webhook could not update tenant plan for customer {customer_id}"
            logging.warning(msg)
            return jsonify(success=False)
        logging.info(
            f"billing_webhook updated customer {customer_id} subscription {subscription_id} status {subscription_status}"
        )
    else:
        # Unexpected event type
        logging.warning(f"billing_webhook got unexpected event {event_type}")

    return jsonify(success=True)


