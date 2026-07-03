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
import asyncio
import logging
import smtplib
import ssl
from typing import Dict, Optional
from datetime import datetime
from concurrent.futures import ThreadPoolExecutor


class SMTPValidator:
    """SMTP connection validator"""

    @staticmethod
    def validate_smtp_connection(host: str, port: int, username: str, password: str, use_tls: bool = True, use_ssl: bool = False, timeout: int = 10) -> Dict[str, any]:
        """
        check SMTP connection

        Returns:
            {
                "success": bool,
                "message": str,
            }
        """
        try:
            logging.info(f"Checking SMTP connection: {host}:{port}")

            # check params
            validation_error = SMTPValidator._validate_params(host, port, username, password)
            if validation_error:
                return {"success": False, "message": f"Check failed: {validation_error}"}

            # choose method
            if use_ssl:
                return SMTPValidator._validate_with_ssl(host, port, username, password, timeout)
            elif use_tls:
                return SMTPValidator._validate_with_tls(host, port, username, password, timeout)
            else:
                return SMTPValidator._validate_without_encryption(host, port, username, password, timeout)

        except Exception as e:
            error_text = f"Check SMTP connection exception: {str(e)}"
            logging.error(error_text)
            return {"success": False, "message": error_text}

    @staticmethod
    def _validate_params(host: str, port: int, username: str, password: str) -> Optional[str]:
        """check parameters"""
        if not host or not host.strip():
            return "SMTP host cannot be empty."

        if port <= 0 or port > 65535:
            return "SMTP port should be between 1 and 65535."

        if not username or not username.strip():
            return "username cannot be empty."

        if not password:
            return "password cannot be empty."

        # port check
        common_ports = [25, 465, 587, 2525]
        if port not in common_ports:
            logging.warning(f"Using uncommon port: {port}")

        return None

    @staticmethod
    def _validate_with_ssl(host: str, port: int, username: str, password: str, timeout: int) -> Dict[str, any]:
        """use ssl to validate"""
        try:
            context = ssl.create_default_context()

            with smtplib.SMTP_SSL(host=host, port=port, timeout=timeout, context=context) as server:
                logging.debug(f"SSL connect succeed: {host}:{port}")

                # try login
                server.login(username, password)
                logging.debug("SMTP login succeed.")
                return {"success": True, "message": "SMTP connection validated successfully."}

        except ssl.SSLError as e:
            error_text = f"SSL error: {str(e)}"
            logging.error(error_text)
            return {"success": False, "message": error_text}
        except smtplib.SMTPAuthenticationError as e:
            error_text = f"SMTP authentication error: {str(e)}"
            logging.error(error_text)
            return {"success": False, "message": error_text}
        except smtplib.SMTPException as e:
            error_text = f"SMTP error: {str(e)}"
            logging.error(error_text)
            return {"success": False, "message": error_text}

    @staticmethod
    def _validate_with_tls(host: str, port: int, username: str, password: str, timeout: int) -> Dict[str, any]:
        """validate with TLS"""
        try:
            with smtplib.SMTP(host=host, port=port, timeout=timeout) as server:
                logging.debug(f"TCP connect succeed: {host}:{port}")

                # send EHLO
                server.ehlo()

                # check if support STARTTLS
                if not server.has_extn("starttls"):
                    return {
                        "success": False,
                        "message": "STARTTLS not supported by server.",
                    }

                # start TLS
                server.starttls()
                server.ehlo()  # resend EHLO

                # try login
                server.login(username, password)
                logging.debug("SMTP login succeed.")
                return {"success": True, "message": "SMTP connection validated successfully."}

        except smtplib.SMTPException as e:
            error_text = f"SMTP error: {str(e)}"
            logging.error(error_text)
            return {"success": False, "message": error_text}

    @staticmethod
    def _validate_without_encryption(host: str, port: int, username: str, password: str, timeout: int) -> Dict[str, any]:
        """check without encryption"""
        try:
            with smtplib.SMTP(host=host, port=port, timeout=timeout) as server:
                logging.warning(f"Using unencrypted connection: {host}:{port}")

                server.ehlo()

                # try login
                server.login(username, password)

                return {"success": True, "message": "SMTP connection validated successfully(unencrypted)."}

        except smtplib.SMTPException as e:
            error_text = f"SMTP exception: {str(e)}"
            logging.error(error_text)
            return {"success": False, "message": error_text}


class AsyncSMTPValidator:
    """async SMTP validator using ThreadPoolExecutor"""

    _executor = ThreadPoolExecutor(max_workers=5)

    @staticmethod
    async def validate_async(host: str, port: int, username: str, password: str, use_tls: bool = True, use_ssl: bool = False, mail_timeout: int = 30) -> Dict[str, any]:
        """
        async check SMTP connection

        Returns:
            {
                "success": bool,
                "message": str,
                "timestamp": str
            }
        """
        try:
            print("call into async", flush=True)
            loop = asyncio.get_event_loop()

            # test connection
            result = await loop.run_in_executor(AsyncSMTPValidator._executor, SMTPValidator.validate_smtp_connection, host, port, username, password, use_tls, use_ssl, mail_timeout)
            print(f"done, {result}", flush=True)
            # add timestamp
            result["timestamp"] = datetime.now().isoformat()

            return result

        except asyncio.TimeoutError:
            return {"success": False, "message": f"Timeout after {mail_timeout} seconds.", "timestamp": datetime.now().isoformat()}
        except Exception as e:
            logging.error(f"Check SMTP connection exception: {str(e)}")
            return {"success": False, "message": f"Check SMTP connection exception: {str(e)}", "timestamp": datetime.now().isoformat()}
