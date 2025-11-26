
import logging
import json
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
                heartbeat=30,
                blocked_connection_timeout=60
            )
            # Establish the connection
            self._channel = pika.BlockingConnection(parameters).channel()
            logging.info("Connect to RabbitMQ: {}".format(self.config["host"]))
        except Exception:
            logging.warning("RabbitMQ can't be connected.")

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
                    "RedisDB.queue_product " + str(routing_key) + " got exception: " + str(e)
                )
                self.__open__()
        return False

    def queue_consumer(self, queue_name, callback):
        self._channel.queue_declare(queue_name, durable=True)
        self._channel.basic_consume(queue=queue_name, on_message_callback=callback, auto_ack=False)
        self._channel.start_consuming()

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
