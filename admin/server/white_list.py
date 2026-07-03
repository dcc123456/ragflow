#
#  Copyright 2025 The InfiniFlow Authors. All Rights Reserved.
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
import re
from typing import Dict, Any

from api.common.exceptions import AdminException
from api.db.services.white_list_service import WhiteListService


class WhiteListMgr:
    @staticmethod
    def get_all_white_list() -> Dict[str, Any]:
        white_list = WhiteListService.get_all_white_lists()
        return {"white_list": white_list, "total": len(white_list)}

    @staticmethod
    def update_white_list_row(row_id, email):
        if not re.match(r"^[\w\._-]+@([\w_-]+\.)+[\w-]{2,}$", email):
            raise AdminException(f"Invalid email address: {email}!")

        exist_email = WhiteListService.get_white_list_by_email(email)
        if exist_email:
            return {
                "success": True,
                "message": f"Email {email} is already in whitelist.",
            }
        _, db_row = WhiteListService.get_by_id(row_id)
        if not db_row:
            raise AdminException(f"White list row not found, id: {row_id}.")
        try:
            WhiteListService.update_white_list_row_by_id(row_id, email)
            return {
                "success": True,
                "message": f"Successfully updated the email from {db_row.email} to {email}.",
            }
        except Exception as e:
            return {
                "success": False,
                "message": str(e),
            }

    @staticmethod
    def create_white_list_row(email):
        if not re.match(r"^[\w\._-]+@([\w_-]+\.)+[\w-]{2,}$", email):
            raise AdminException(f"Invalid email address: {email}!")
        exist_email = WhiteListService.get_white_list_by_email(email)
        if exist_email:
            return {
                "success": True,
                "message": f"Email {email} is already in whitelist.",
            }
        try:
            WhiteListService.create_white_list_row(email)
            return {
                "success": True,
                "message": f"Successfully added email {email} to whitelist.",
            }
        except Exception as e:
            return {
                "success": False,
                "message": str(e),
            }

    @staticmethod
    def delete_email_from_white_list(email):
        exist_email = WhiteListService.get_white_list_by_email(email)
        if not exist_email:
            return {
                "success": True,
                "message": f"Email {email} is not in whitelist.",
            }
        try:
            WhiteListService.delete_by_id(exist_email.id)
            return {"success": True, "message": f"Successfully deleted email {email} from whitelist."}
        except Exception as e:
            return {
                "success": False,
                "message": str(e),
            }

    @staticmethod
    def batch_create_white_list_rows(emails):
        if not emails:
            raise AdminException("Email list is empty.")
        error_addr = []
        for email in emails:
            if not re.match(r"^[\w\._-]+@([\w_-]+\.)+[\w-]{2,}$", email):
                error_addr.append(email)
        if error_addr:
            raise AdminException(f"Invalid email address: {', '.join(error_addr)}")
        try:
            insert_cnt = WhiteListService.batch_create_white_list_row(emails)
            email_noun = "email" if insert_cnt == 1 else "emails"
            already_exist_email_cnt = len(emails) - insert_cnt
            already_exist_email_noun = "email" if already_exist_email_cnt == 1 else "emails"
            return {
                "success": True,
                "message": f"Upload Successfully! Batch added {insert_cnt} {email_noun}, {already_exist_email_cnt} {already_exist_email_noun} already in whitelist. ",
            }
        except Exception as e:
            return {
                "success": False,
                "message": str(e),
            }
