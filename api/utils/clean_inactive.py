from datetime import datetime
from tqdm import tqdm
from api.db.services.knowledgebase_service import KnowledgebaseService
from api.db.services.document_service import DocumentService
from api.db.services.file_service import FileService
from api.db.db_models import Document, File, Conversation, UserCanvas
from api.db.services.canvas_service import UserCanvasService
from api.db.services.conversation_service import ConversationService
from api.db.services.dialog_service import DialogService
from rag.nlp import search
from api.db.services.user_service import UserService
from common import settings
from common.settings import STORAGE_IMPL

settings.init_settings()


def delete_chunk(date_str, n, m):
    date = datetime.strptime(date_str, "%Y-%m-%d")
    users = UserService.get_all()

    # user ids before date
    user_ids = set()
    for user in users:
        if user.update_date < date and user.update_time % m == n:
            user_ids.add(user.id)

    print("Users: ", len(user_ids))
    for uid in tqdm(user_ids):
        try:
            settings.docStoreConn.delete_idx(search.index_name(uid), "")
        except Exception as e:
            print("ES: ", e)
        kb_ids = KnowledgebaseService.get_kb_ids(uid)
        for kid in kb_ids:
            try:
                STORAGE_IMPL.rm_bucket(kid)
            except Exception as e:
                print("MinIO: ", e)
            try:
                for d in DocumentService.query(kb_id=kid):
                    Document.delete_by_id(d.id)
            except Exception as e:
                print(e)

            # Knowledgebase.delete_by_id(kid)

        try:
            for file in FileService.query(tenant_id=uid):
                File.delete_by_id(file.id)
            for dia in DialogService.query(tenant_id=uid):
                for con in ConversationService.query(dialog_id=dia.id):
                    Conversation.delete_by_id(con.id)

            for canv in UserCanvasService.query(user_id=uid):
                UserCanvas.delete_by_id(canv.id)
        except Exception as e:
            print(e)


if __name__ == "__main__":
    delete_chunk("2024-08-10", 0, 4)
