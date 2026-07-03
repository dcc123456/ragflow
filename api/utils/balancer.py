from urllib.parse import urlsplit, urlunsplit
from threading import Thread, Lock
import copy
import time
import logging
import requests


class LoadBalancer(object):
    """
    Load Balancer for Python using Round Robin algorithm.
    It will check the health of the instances every interval seconds.
    """

    def __init__(self, instances, healthcheck_path="/health", interval=30):
        self.instances = sorted(instances)
        self.instances_health = []
        for i in range(len(instances)):
            scheme, netloc, _, query, fragment = urlsplit(self.instances[i])
            new_url = urlunsplit((scheme, netloc, healthcheck_path, query, fragment))
            self.instances_health.append(new_url)
        self.interval = interval
        self.lock = Lock()
        self.alive_instances = copy.deepcopy(self.instances)
        self.next_index = 0
        self.checker = Thread(target=self.check_alive, daemon=True)
        self.checker.start()

    def get_instance(self):
        return self.instances[0]
        """Get an alive instance"""
        with self.lock:
            return self.instances[0]
            if len(self.alive_instances) == 0:
                return None
            instance = self.alive_instances[self.next_index]
            self.next_index = (self.next_index + 1) % len(self.alive_instances)
            return instance

    def exchange_instance(self, instance):
        """Mark an instance dead and get another alive instance"""
        with self.lock:
            self.alive_instances.remove(instance)
            if len(self.alive_instances) == 0:
                return None
            self.next_index = self.next_index % len(self.alive_instances)
            instance2 = self.alive_instances[self.next_index]
            self.next_index = (self.next_index + 1) % len(self.alive_instances)
            return instance2

    def get_all_instances(self):
        return self.instances

    def get_alive_instances(self):
        with self.lock:
            tmp_alive_instances = copy.deepcopy(self.alive_instances)
        return tmp_alive_instances

    def check_alive(self):
        while True:
            tmp_alive_instances = []
            for i in range(len(self.instances)):
                try:
                    requests.post(self.instances[i], json={"sentences": ["Healthy?"]})
                    # if response.status_code == 200:
                    tmp_alive_instances.append(self.instances[i])
                except Exception as e:
                    logging.warning(f"Instance {self.instances[i]} is not alive, exception {str(e)}.")
            with self.lock:
                self.alive_instances = tmp_alive_instances
                if self.alive_instances:
                    self.next_index = self.next_index % len(self.alive_instances)
                else:
                    self.next_index = 0
            time.sleep(self.interval)
