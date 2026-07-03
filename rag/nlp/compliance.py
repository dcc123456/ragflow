import os
import logging
from typing import Union, List

from api.utils.file_utils import get_project_base_directory
from rag.nlp import rag_tokenizer


class Dealer:
    def __init__(self):
        self.dictionary = set([])
        path = os.path.join(get_project_base_directory(), "rag/res", "compliance.txt")
        try:
            with open(path, "r") as f:
                while True:
                    line = f.readline()
                    if not line:
                        break
                    line = line.strip("\n")
                    self.dictionary.add(line.lower())
        except Exception:
            logging.warning("Missing compliance.txt")

    def lookup(self, txt: Union[str, List]) -> List:
        return []
        if not self.dictionary:
            return []

        tks = txt if isinstance(txt, list) else rag_tokenizer.tokenize(txt).split(" ")
        for j in range(1, 6):
            s = set(["".join([tks[i + ii] for ii in range(j)]) for i in range(len(tks) - j + 1)]) | set([" ".join([tks[i + ii] for ii in range(j)]) for i in range(len(tks) - j + 1)])
            comm = set(s) & self.dictionary
            if comm:
                return list(comm)

            if len(tks) < j + 1:
                return []
        return []


if __name__ == "__main__":
    dl = Dealer()
    print(dl.dictionary)
