import os
import sys
import trio
from tqdm import tqdm
from api.db.services.document_service import DocumentService
from api.db.db_models import Document
from common import settings
from common.settings import STORAGE_IMPL
from rag.app.naive import Pdf

settings.init_settings()


def find_pdfs(root_dir):
    pdf_files = []
    for dirpath, _, filenames in os.walk(root_dir):
        for filename in filenames:
            if filename.lower().endswith(".pdf"):
                full_path = os.path.join(dirpath, filename)
                pdf_files.append(full_path)
    return pdf_files


def dummy(prog=None, msg=""):
    pass


DOCS = DocumentService.model.select(*[Document.name, Document.location, Document.kb_id]).where(Document.name.endswith(".pdf")).order_by(Document.getter_by("create_time").desc())
PAGE_SIZE = 10
st_page = 0
with open("logs/pages.txt", "r") as f:
    while True:
        line = f.readline()
        if not line:
            break
        st_page = int(line.strip()) + PAGE_SIZE + 10

for page in tqdm(range(st_page, DOCS.count(), PAGE_SIZE)):
    p = page
    for doc in DOCS[page : page + PAGE_SIZE]:
        p += PAGE_SIZE
        tenant_id = DocumentService.get_tenant_id_by_name(doc.name)
        try:
            if not STORAGE_IMPL.obj_exist(doc.kb_id, doc.location, tenant_id):
                continue
            bin = STORAGE_IMPL.get(doc.kb_id, doc.location, tenant_id)
            _, tbls, _ = Pdf()(None, binary=bin, callback=dummy, separate_tables_figures=True)

            for j, ((img, rows), poss) in enumerate(tbls):
                img.save(os.path.join(sys.argv[1], str(hash(f"{doc.location}-{j}.jpg")) + ".jpg"))
                img.close()
        except Exception as e:
            print(e)
        with open("logs/pages.txt", "a+") as f:
            f.write(f"{p}\n")
sys.exit()


async def extract(n, m):
    global DOCS, PAGE_SIZE

    # user ids before date
    for page in tqdm(range(0, min(DOCS.count(), 500), PAGE_SIZE)):
        if page % m != n:
            continue
        for doc in DOCS.paginate(page, PAGE_SIZE):
            tenant_id = DocumentService.get_tenant_id_by_name(doc.name)
            try:
                if not STORAGE_IMPL.obj_exist(doc.kb_id, doc.location, tenant_id):
                    continue
                bin = STORAGE_IMPL.get(doc.kb_id, doc.location, tenant_id)
                _, tbls, _ = Pdf()(None, binary=bin, callback=dummy, separate_tables_figures=True)

                for j, ((img, rows), poss) in enumerate(tbls):
                    img.save(os.path.join(sys.argv[1], str(hash(f"{doc.location}-{j}.jpg")) + ".jpg"))
                    img.close()
            except Exception as e:
                print(e)


async def main():
    async with trio.open_nursery() as nursery:
        for i in range(4):
            nursery.start_soon(extract, i, 4)


if __name__ == "__main__":
    trio.run(main)
    sys.exit()

    pdfs = find_pdfs(sys.argv[1])
    for pdf in pdfs:
        _, tbls, _ = Pdf()(pdf, callback=dummy, separate_tables_figures=True)

        for j, ((img, rows), poss) in enumerate(tbls):
            img.save(os.path.join(sys.argv[2], f"{pdf}-{j}.jpg"))
