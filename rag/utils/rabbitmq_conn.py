
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
            logging.info("Connect to RabbitMQ: {}".format(self.config["host"]))
        except Exception:
            logging.warning("RabbitMQ can't be connected.")

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

    def queue_product(self, routing_key:str, message:dict) -> bool:
        for _ in range(3):
            try:
                self._channel.basic_publish(exchange=self.config["exchange"], routing_key=routing_key, body=json.dumps(message))
                return True
            except Exception as e:
                logging.exception(
                    "RabbitMQ.queue_product " + str(routing_key) + " got exception: " + str(e)
                )
                self.__open__()
        return False

    def queue_consumer(self, queue_name, callback):
        """
        Consumer that runs callbacks in a separate thread to prevent heartbeat timeout.
        See: https://github.com/pika/pika/issues/1104#issuecomment-407358142

        Key fix: Use threading for callback execution but don't block the main thread.
        Use a result queue to communicate ack decisions back to main thread.
        """
        import threading
        import queue

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

            # Process network events
            self._channel.connection.process_data_events(time_limit=1)
            self._channel.connection.sleep(0.5)

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
