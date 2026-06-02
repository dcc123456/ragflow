
import logging
import json
import signal
import sys
import threading
import pika
import requests
from requests.auth import HTTPBasicAuth

from common import settings
from common.decorator import singleton
from common.config_utils import get_base_config


@singleton
class RabbitQueue:

    def __init__(self):
        self._channel = None
        self._conn = None
        self._publisher = threading.local()
        self._setup_signal_handlers()
        self.config = settings.RABBIT_CONF if settings.RABBIT_CONF else get_base_config("rabbitmq")

    def _connection_parameters(self):
        credentials = pika.PlainCredentials(self.config["user"], self.config["password"])
        return pika.ConnectionParameters(
            host=self.config["host"],
            port=int(self.config["port"]), # Default AMQP port
            credentials=credentials,
            socket_timeout=10,
            heartbeat=0,  # Disabled - using manual heartbeat in queue_consumer
            blocked_connection_timeout=60*60*2
        )

    def __open__(self):
        # Close existing connection before creating new one
        self._close_connection()
        try:
            # Establish the connection
            self._conn = pika.BlockingConnection(self._connection_parameters())
            self._channel = self._conn.channel()
            logging.info("Connect to RabbitMQ: {}".format(self.config))
        except Exception:
            logging.warning("RabbitMQ can't be connected.")

    def _ensure_connection(self):
        """Open the shared connection lazily when queue operations need it."""
        if self._is_connection_healthy():
            return True
        self.__open__()
        return self._is_connection_healthy()

    def _close_connection(self):
        """Safely close existing connection and channel."""
        try:
            if self._channel and self._channel.is_open:
                self._channel.close()
        except Exception:
            pass
        try:
            if self._conn and self._conn.is_open:
                self._conn.close()
        except Exception:
            pass
        self._channel = None
        self._conn = None

    def _close_publisher_connection(self):
        """Close the publisher connection owned by the current thread."""
        try:
            channel = getattr(self._publisher, "channel", None)
            if channel and channel.is_open:
                channel.close()
        except Exception:
            pass
        try:
            conn = getattr(self._publisher, "conn", None)
            if conn and conn.is_open:
                conn.close()
        except Exception:
            pass
        self._publisher.channel = None
        self._publisher.conn = None

    def _get_publisher_channel(self):
        """Return a thread-local channel for publishing.

        pika BlockingConnection/BlockingChannel objects are not thread-safe.
        The consumer loop uses self._channel, while task callbacks may publish
        follow-up work from worker threads.
        """
        conn = getattr(self._publisher, "conn", None)
        channel = getattr(self._publisher, "channel", None)
        if conn and conn.is_open and channel and channel.is_open:
            return channel

        self._close_publisher_connection()
        if not self._ensure_connection():
            raise ConnectionError("RabbitMQ shared connection is unavailable.")
        conn = pika.BlockingConnection(self._connection_parameters())
        channel = conn.channel()
        self._publisher.conn = conn
        self._publisher.channel = channel
        return channel

    def _setup_signal_handlers(self):
        def signal_handler(signum, frame):
            print(f"Received {signum}，closing...")
            self._close_publisher_connection()
            self._close_connection()
            sys.exit(0)

        signal.signal(signal.SIGTERM, signal_handler)  # kill
        signal.signal(signal.SIGINT, signal_handler)

    def health(self):
        return True

    def is_alive(self):
        username = settings.RABBIT_CONF["user"]
        password = settings.RABBIT_CONF["password"]
        host = settings.RABBIT_CONF["host"]
        port = settings.RABBIT_CONF["api_port"]
        url = f'http://{host}:{port}/api/aliveness-test/%2F'
        try:
            response = requests.get(url, auth=(username, password))
            if response.status_code == 200:
                result = response.json()
                if result.get("status") == "ok":
                    return True
                else:
                    return False
            else:
                return False
        except Exception as e:
            logging.error(e)
            return False

    def _estimate_size(self, obj, depth=0):
        """Estimate JSON serialization size without actually serializing."""
        if depth > 10:
            return 100
        if isinstance(obj, dict):
            return sum(2 + len(str(k)) + self._estimate_size(v, depth+1) for k, v in obj.items())
        elif isinstance(obj, list):
            return 2 + sum(self._estimate_size(item, depth+1) for item in obj)
        elif isinstance(obj, str):
            return len(obj)
        elif isinstance(obj, (int, float, bool)):
            return 20
        else:
            return str(obj).__len__() if hasattr(obj, '__len__') else 50

    def queue_product(self, routing_key:str, message:dict) -> bool:
        # Estimate size before serialization to catch large messages early
        estimated_size = self._estimate_size(message)
        if estimated_size > 10 * 1000:
            logging.warning(f"Large message estimated {estimated_size} bytes for {routing_key}")

        for i in range(3):
            try:
                body = json.dumps(message)
                if len(body) > 10 * 1000:  # 10KB
                    logging.warning(f"Large message for {routing_key}: {len(body)} bytes")
                # Ensure the queue exists and is bound to the exchange
                channel = self._get_publisher_channel()
                channel.queue_declare(routing_key, durable=True) # routing_key == queue_name
                channel.queue_bind(queue=routing_key, exchange=self.config["exchange"], routing_key=routing_key)
                channel.basic_publish(exchange=self.config["exchange"], routing_key=routing_key, body=body)
                return True
            except Exception as e:
                logging.exception(
                    "RabbitMQ.queue_product " + str(routing_key) + " got exception: " + str(e)
                )
                self._close_publisher_connection()
                import time
                time.sleep(0.5 * (i + 1))  # Exponential backoff
        return False

    def _is_connection_healthy(self):
        """Check if the RabbitMQ connection is healthy.

        Returns True if connection and channel are open, False otherwise.
        """
        try:
            if not self._conn or not self._conn.is_open:
                return False
            if not self._channel or not self._channel.is_open:
                return False
            return True
        except Exception:
            return False

    def queue_consumer(self, queue_name, callback):
        """
        Consumer that runs callbacks in a separate thread to prevent heartbeat timeout.
        See: https://github.com/pika/pika/issues/1104#issuecomment-407358142

        Key fix: Use threading for callback execution but don't block the main thread.
        Use a result queue to communicate ack decisions back to main thread.

        Includes retry logic with reconnection on connection errors.
        Enhanced with connection health checks to prevent working with corrupted connections.
        """
        import threading
        import queue
        import time

        consecutive_failures = 0
        max_consecutive_failures = 5

        while True:
            try:
                # Ensure we have a valid connection with health check
                if not self._is_connection_healthy():
                    logging.info("Connection unhealthy, reconnecting...")
                    self._close_connection()
                    self._ensure_connection()

                if not self._is_connection_healthy():
                    logging.warning("Failed to establish healthy connection, retrying...")
                    time.sleep(2)
                    continue

                # Declare queue first
                self._channel.queue_declare(queue_name, durable=True)
                self._channel.basic_qos(prefetch_count=1)

                # Use thread-safe queue for callback results
                result_queue = queue.Queue()

                def threaded_callback(ch, method, properties, body):
                    delivery_tag = method.delivery_tag
                    should_ack = False

                    def run_callback():
                        nonlocal should_ack
                        try:
                            # Callback can return True (ack) or False (nack), or None (default ack)
                            result = callback(ch, method, properties, body)
                            should_ack = result if result is not None else True
                        except Exception as e:
                            logging.warning(f"Callback exception: {e}")
                            should_ack = True  # Ack on exception to avoid message redelivery
                        finally:
                            # Put result back to main thread
                            result_queue.put((delivery_tag, should_ack))

                    # Start callback in separate thread and return immediately
                    # Don't wait for callback to complete - let main thread handle events
                    t = threading.Thread(target=run_callback, daemon=True)
                    t.start()

                self._channel.basic_consume(queue=queue_name, on_message_callback=threaded_callback, auto_ack=False)

                # Reset consecutive failures on successful consume setup
                consecutive_failures = 0

                # Process events in main thread - this handles heartbeat and ack/nack
                while self._channel.consumer_tags:
                    # Process pending results from callback threads
                    while True:
                        try:
                            tag, should_ack = result_queue.get_nowait()
                            try:
                                if should_ack:
                                    self._channel.basic_ack(delivery_tag=tag)
                                else:
                                    self._channel.basic_nack(delivery_tag=tag, requeue=True)
                            except Exception as e:
                                logging.warning(f"Failed to ack/nack delivery tag {tag}: {e}")
                        except queue.Empty:
                            break

                    # Check connection health periodically
                    if not self._is_connection_healthy():
                        logging.warning("Connection became unhealthy during consume loop")
                        break

                    # Process network events with error handling
                    try:
                        self._channel.connection.process_data_events(time_limit=1)
                    except Exception as e:
                        logging.warning(f"process_data_events failed: {e}")
                        break

                    self._channel.connection.sleep(0.5)

            except Exception as e:
                consecutive_failures += 1
                logging.warning(f"queue_consumer {queue_name} exception: {e} (failure {consecutive_failures}/{max_consecutive_failures})")

                # Force reconnection if too many consecutive failures
                self._close_connection()

                if consecutive_failures >= max_consecutive_failures:
                    logging.error(f"Too many consecutive failures ({consecutive_failures}), taking longer break...")
                    time.sleep(10)
                    consecutive_failures = 0
                else:
                    time.sleep(1)  # Wait before reconnecting

    def priority_queue_consumer(self, high_priority_queue, low_priority_queue, callback, max_concurrency=1):
        """
        Consume from two queues with priority-based dispatch.
        Always checks the high-priority queue first; only consumes from the
        low-priority queue when no high-priority messages are available.

        Uses basic_get polling so the consumer retains full control over which
        queue to read next, unlike basic_consume which delivers messages in
        the order they arrive across all subscribed queues.

        Callbacks are executed in separate threads to prevent pika heartbeat
        timeouts during long-running task processing. At most max_concurrency
        messages are processed at the same time.
        """
        import threading
        import queue as queue_mod
        import time

        consecutive_failures = 0
        max_consecutive_failures = 5
        IDLE_POLL_INTERVAL = 0.5  # seconds to sleep when both queues are empty
        max_concurrency = max(1, int(max_concurrency))

        while True:
            try:
                # Ensure we have a valid connection
                if not self._is_connection_healthy():
                    logging.info("Connection unhealthy, reconnecting...")
                    self._close_connection()
                    self._ensure_connection()

                if not self._is_connection_healthy():
                    logging.warning("Failed to establish healthy connection, retrying...")
                    time.sleep(2)
                    continue

                # Declare both queues
                self._channel.queue_declare(high_priority_queue, durable=True)
                self._channel.queue_declare(low_priority_queue, durable=True)

                consecutive_failures = 0
                result_queue = queue_mod.Queue()
                active_threads = {}

                while True:
                    if not self._is_connection_healthy():
                        logging.warning("Connection became unhealthy during priority consume loop")
                        break

                    def run_callback(ch, mtd, props, bdy, src_q):
                        should_ack = True
                        try:
                            result = callback(ch, mtd, props, bdy)
                            should_ack = result if result is not None else True
                        except Exception as e:
                            logging.warning(f"Callback exception for message from {src_q}: {e}")
                            should_ack = True
                        finally:
                            result_queue.put((mtd.delivery_tag, should_ack))

                    # Fill available slots, always checking high-priority first.
                    while len(active_threads) < max_concurrency:
                        method_frame = self._channel.basic_get(queue=high_priority_queue, auto_ack=False)
                        source_queue = high_priority_queue

                        if method_frame[0] is None:
                            method_frame = self._channel.basic_get(queue=low_priority_queue, auto_ack=False)
                            source_queue = low_priority_queue

                        method, properties, body = method_frame

                        if method is None:
                            break

                        delivery_tag = method.delivery_tag
                        t = threading.Thread(
                            target=run_callback,
                            args=(self._channel, method, properties, body, source_queue),
                            daemon=True,
                        )
                        active_threads[delivery_tag] = t
                        t.start()

                    processed_result = False
                    while True:
                        try:
                            delivery_tag, should_ack = result_queue.get_nowait()
                        except queue_mod.Empty:
                            break

                        processed_result = True
                        active_threads.pop(delivery_tag, None)
                        try:
                            if should_ack:
                                self._channel.basic_ack(delivery_tag=delivery_tag)
                            else:
                                self._channel.basic_nack(delivery_tag=delivery_tag, requeue=True)
                        except Exception as e:
                            logging.warning(f"Failed to ack/nack delivery tag {delivery_tag}: {e}")

                    try:
                        self._channel.connection.process_data_events(time_limit=0.1)
                    except Exception:
                        logging.warning("process_data_events failed during priority consume loop")
                        break

                    if not active_threads and not processed_result:
                        time.sleep(IDLE_POLL_INTERVAL)

            except Exception as e:
                consecutive_failures += 1
                logging.warning(
                    f"priority_queue_consumer exception: {e} "
                    f"(failure {consecutive_failures}/{max_consecutive_failures})"
                )
                self._close_connection()

                if consecutive_failures >= max_consecutive_failures:
                    logging.error(f"Too many consecutive failures ({consecutive_failures}), taking longer break...")
                    time.sleep(10)
                    consecutive_failures = 0
                else:
                    time.sleep(1)

    def get_queue_length(self, queue_name, vhost: str = "/") -> int:
        for _ in range(3):
            try:
                host = self.config["host"]
                port = self.config["api_port"]
                user = self.config["user"]
                password = self.config["password"]
                url_vhost = requests.utils.quote(vhost, safe='')
                url = f"http://{host}:{port}/api/queues/{url_vhost}/{queue_name}"

                response = requests.get(url, auth=HTTPBasicAuth(user, password), timeout=5)
                if response.status_code == 200:
                    data = response.json()
                    return data.get("messages", 0)
                else:
                    logging.error(f"Failed to get queue info: {response.status_code} - {response.text}")
                q = self._channel.queue_declare(queue_name, durable=True)
                return q.method.message_count
            except Exception as e:
                logging.exception(e)
                self._ensure_connection()
        return 110

    
RABBITMQ_CONN = RabbitQueue()


async def async_get_queue_status(queue_name):
    import httpx

    try:
        username = settings.RABBIT_CONF["user"]
        password = settings.RABBIT_CONF["password"]
        host = settings.RABBIT_CONF["host"]
        port = settings.RABBIT_CONF["api_port"]

        async with httpx.AsyncClient(timeout=10.0) as client:
            # 使用 Basic Auth
            auth = (username, password)
            queue_name_encoded = httpx.URL(queue_name).path
            url = f'http://{host}:{port}/api/queues/%2F/{queue_name_encoded}'

            response = await client.get(url, auth=auth)

            if response.status_code == 200:
                data = response.json()
                return {
                    "messages_ready": data.get('messages_ready', 0),
                    "messages_unacknowledged": data.get('messages_unacknowledged', 0),
                    "messages_total": data.get('messages', 0),
                    "consumer_count": data.get('consumers', 0),
                    "state": data.get('state', 'unknown'),
                    "memory": data.get('memory', 0)
                }
            elif response.status_code == 404:
                logging.warning(f"Queue {queue_name} not found")
                return None
            else:
                logging.error(f"HTTP {response.status_code}: {response.text}")
                return None

    except httpx.TimeoutException:
        logging.error("HTTP request timeout when getting queue stats")
        return None
    except httpx.RequestError as e:
        logging.error(f"HTTP request failed: {e}")
        return None
    except Exception as e:
        logging.error(f"Unexpected error in get_queue_stats_via_httpx: {e}")
        return None
