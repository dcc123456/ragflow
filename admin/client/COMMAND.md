# RAGFlow CLI User Command Reference

This document describes the user commands available in RAGFlow CLI. All commands must end with a semicolon (`;`).

## Command List

### ping_server

**Description**  
Tests the connection status to the server.

**Usage**  
```
PING;
```

**Parameters**  
No parameters.

**Example**  
```
ragflow> PING;
```

**Display Effect**  
(Sample output will be provided by the user)

---

### show_current_user

**Description**  
Displays information about the currently logged-in user.

**Usage**  
```
SHOW CURRENT USER;
```

**Parameters**  
No parameters.

**Example**  
```
ragflow> SHOW CURRENT USER;
```

**Display Effect**  
(Sample output will be provided by the user)

---

### create_user_dataset_with_parser

**Description**  
Creates a user dataset with the specified parser.

**Usage**  
```
CREATE DATASET <dataset_name> WITH EMBEDDING <embedding> PARSER <parser_type>;
```

**Parameters**  
- `dataset_name`: Dataset name, quoted string.
- `embedding`: Embedding model name, quoted string.
- `parser_type`: Parser type, quoted string.

**Example**  
```
ragflow> CREATE DATASET 'my_dataset' WITH EMBEDDING 'text-embedding-ada-002' PARSER 'pdf';
```

**Display Effect**  
(Sample output will be provided by the user)

---

### create_user_dataset_with_pipeline

**Description**  
Creates a user dataset with the specified pipeline.

**Usage**  
```
CREATE DATASET <dataset_name> WITH EMBEDDING <embedding> PIPELINE <pipeline>;
```

**Parameters**  
- `dataset_name`: Dataset name, quoted string.
- `embedding`: Embedding model name, quoted string.
- `pipeline`: Pipeline name, quoted string.

**Example**  
```
ragflow> CREATE DATASET 'my_dataset' WITH EMBEDDING 'text-embedding-ada-002' PIPELINE 'standard';
```

**Display Effect**  
(Sample output will be provided by the user)

---

### drop_user_dataset

**Description**  
Deletes a user dataset.

**Usage**  
```
DROP DATASET <dataset_name>;
```

**Parameters**  
- `dataset_name`: Name of the dataset to delete, quoted string.

**Example**  
```
ragflow> DROP DATASET 'my_dataset';
```

**Display Effect**  
(Sample output will be provided by the user)

---

### list_user_datasets

**Description**  
Lists all datasets for the current user.

**Usage**  
```
LIST DATASETS;
```

**Parameters**  
No parameters.

**Example**  
```
ragflow> LIST DATASETS;
```

**Display Effect**  
(Sample output will be provided by the user)

---

### list_user_dataset_files

**Description**  
Lists all files in the specified dataset.

**Usage**  
```
LIST FILES OF DATASET <dataset_name>;
```

**Parameters**  
- `dataset_name`: Dataset name, quoted string.

**Example**  
```
ragflow> LIST FILES OF DATASET 'my_dataset';
```

**Display Effect**  
(Sample output will be provided by the user)

---

### list_user_agents

**Description**  
Lists all agents for the current user.

**Usage**  
```
LIST AGENTS;
```

**Parameters**  
No parameters.

**Example**  
```
ragflow> LIST AGENTS;
```

**Display Effect**  
(Sample output will be provided by the user)

---

### list_user_chats

**Description**  
Lists all chat sessions for the current user.

**Usage**  
```
LIST CHATS;
```

**Parameters**  
No parameters.

**Example**  
```
ragflow> LIST CHATS;
```

**Display Effect**  
(Sample output will be provided by the user)

---

### create_user_chat

**Description**  
Creates a new chat session.

**Usage**  
```
CREATE CHAT <chat_name>;
```

**Parameters**  
- `chat_name`: Chat session name, quoted string.

**Example**  
```
ragflow> CREATE CHAT 'my_chat';
```

**Display Effect**  
(Sample output will be provided by the user)

---

### drop_user_chat

**Description**  
Deletes a chat session.

**Usage**  
```
DROP CHAT <chat_name>;
```

**Parameters**  
- `chat_name`: Name of the chat session to delete, quoted string.

**Example**  
```
ragflow> DROP CHAT 'my_chat';
```

**Display Effect**  
(Sample output will be provided by the user)

---

### import_docs_into_dataset

**Description**  
Imports documents into the specified dataset.

**Usage**  
```
IMPORT <document_list> INTO DATASET <dataset_name>;
```

**Parameters**  
- `document_list`: List of document paths, multiple paths can be separated by commas, or as a space-separated quoted string.
- `dataset_name`: Target dataset name, quoted string.

**Example**  
```
ragflow> IMPORT '/path/to/doc1.pdf,/path/to/doc2.pdf' INTO DATASET 'my_dataset';
```

**Display Effect**  
(Sample output will be provided by the user)

---

### search_on_datasets

**Description**  
Searches in one or more specified datasets.

**Usage**  
```
SEARCH <question> ON DATASETS <dataset_list>;
```

**Parameters**  
- `question`: Search question, quoted string.
- `dataset_list`: List of dataset names, multiple names can be separated by commas, or as a space-separated quoted string.

**Example**  
```
ragflow> SEARCH 'What is RAG?' ON DATASETS 'dataset1,dataset2';
```

**Display Effect**  
(Sample output will be provided by the user)

---

### parse_dataset_docs

**Description**  
Parses specified documents in a dataset.

**Usage**  
```
PARSE <document_names> OF DATASET <dataset_name>;
```

**Parameters**  
- `document_names`: List of document names, multiple names can be separated by commas, or as a space-separated quoted string.
- `dataset_name`: Dataset name, quoted string.

**Example**  
```
ragflow> PARSE 'doc1.pdf,doc2.pdf' OF DATASET 'my_dataset';
```

**Display Effect**  
(Sample output will be provided by the user)

---

### parse_dataset_sync

**Description**  
Synchronously parses the entire dataset.

**Usage**  
```
PARSE DATASET <dataset_name> SYNC;
```

**Parameters**  
- `dataset_name`: Dataset name, quoted string.

**Example**  
```
ragflow> PARSE DATASET 'my_dataset' SYNC;
```

**Display Effect**  
(Sample output will be provided by the user)

---

### parse_dataset_async

**Description**  
Asynchronously parses the entire dataset.

**Usage**  
```
PARSE DATASET <dataset_name> ASYNC;
```

**Parameters**  
- `dataset_name`: Dataset name, quoted string.

**Example**  
```
ragflow> PARSE DATASET 'my_dataset' ASYNC;
```

**Display Effect**  
(Sample output will be provided by the user)

---

### benchmark

**Description**  
Performs performance benchmark testing on the specified user command.

**Usage**  
```
BENCHMARK <concurrency> <iterations> <user_command>;
```

**Parameters**  
- `concurrency`: Concurrency number, positive integer.
- `iterations`: Number of iterations, positive integer.
- `user_command`: User command to test (must be a valid user command, such as `PING;`).

**Example**  
```
ragflow> BENCHMARK 5 10 PING;
```

**Display Effect**  
(Sample output will be provided by the user)

---

**Notes**  
- All string parameters (such as names, IDs, paths) must be enclosed in single quotes (`'`) or double quotes (`"`).
- Commands must end with a semicolon (`;`).
- The prompt is `ragflow>`.
