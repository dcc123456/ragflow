
import logging
import json
import pika
import requests
from requests.auth import HTTPBasicAuth

from rag import settings
from rag.utils import singleton
from valkey.lock import Lock


@singleton
class RabbitQueue:

    def __init__(self):
        self._channel = None
        self.config = settings.RABBIT_CONF
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
        return self._channel

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
