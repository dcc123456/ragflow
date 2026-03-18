
import logging
import json
import signal
import sys
import pika
import requests
from requests.auth import HTTPBasicAuth

from common import settings
from common.decorator import singleton
from common.config_utils import get_base_config


@singleton
class RabbitQueue:

    def __init__(self):
        self._setup_signal_handlers()
        self._channel = None
        self._conn = None
        self.config = settings.RABBIT_CONF if settings.RABBIT_CONF else get_base_config("rabbitmq")
        self.__open__()

    def __open__(self):
        # Close existing connection before creating new one
        self._close_connection()
        try:
            credentials = pika.PlainCredentials(self.config["user"], self.config["password"])
            parameters = pika.ConnectionParameters(
                host=self.config["host"],
                port=int(self.config["port"]), # Default AMQP port
                credentials=credentials,
                socket_timeout=10,
                heartbeat=0,  # Disabled - using manual heartbeat in queue_consumer
                blocked_connection_timeout=60*60*2
            )
            # Establish the connection
            self._conn = pika.BlockingConnection(parameters)
            self._channel = self._conn.channel()
            logging.info("Connect to RabbitMQ: {}".format(self.config))
        except Exception:
            logging.warning("RabbitMQ can't be connected.")

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

    def _setup_signal_handlers(self):
        def signal_handler(signum, frame):
            print(f"Received {signum}，closing...")
            self._conn.close()
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
        if estimated_size > 10 * 1024:
            logging.warning(f"Large message estimated {estimated_size} bytes for {routing_key}")

        for i in range(3):
            try:
                body = json.dumps(message)
                if len(body) > 10 * 1024:  # 10KB
                    logging.warning(f"Large message for {routing_key}: {len(body)} bytes")
                self._channel.basic_publish(exchange=self.config["exchange"], routing_key=routing_key, body=body)
                return True
            except Exception as e:
                logging.exception(
                    "RabbitMQ.queue_product " + str(routing_key) + " got exception: " + str(e)
                )
                # Only reconnect after exception to avoid corrupted connection state
                self._close_connection()
                self.__open__()
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
                    self.__open__()

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
                self.__open__()
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
