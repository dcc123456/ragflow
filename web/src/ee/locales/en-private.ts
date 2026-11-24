/**
 * Enterprise customization overlay for `en.ts` (replaces the former
 * `locales/en-private.ts`). Deep-merged over `@/locales/en` via
 * `locales/config.ts`; every key here overrides or extends the upstream
 * file, which stays byte-identical to upstream.
 */
export default {
  translation: {
    chunk: {
      versionHistory: 'Version history',
      // The head version, i.e. the one the chunk is at. A restore appends a new
      // version, so the newest row is also the current state; content history
      // says "Current" (datasetVersion.current) while "Latest" stays with
      // published artifacts such as agent and pipeline versions.
      currentVersion: 'Current',
      noVersions: 'No versions yet. Edit this chunk to start its history.',
      versionEmpty: 'This version stored no content.',
      // A removal marker holds no state: both panes say what it records.
      versionChunkDeleted: 'The chunk is deleted in this version.',
      restoredFrom: 'Restored from v{{version}}',
      // A restore the dataset versioning replayed says so in the same line: what
      // triggered it is part of where the version came from.
      restoredFromDataset:
        'Restored from v{{version}} (dataset version restore)',
      // The dataset version the row belongs to, so the reader can place it on the
      // dataset timeline; the row already says the change came from a replay.
      restoredFromDatasetVersion:
        'Restored from v{{version}} (dataset version: {{dataset}})',
      restoreVersion: 'Restore',
      restoreVersionTitle: 'Restore v{{version}}?',
      restoreVersionDescription:
        'The chunk takes the text of v{{version}} back. The restore is recorded as a new version, so every state the history already holds is kept.',
      versionRestored: 'Version restored',
      changedBy: 'By {{user}}',
      systemActor: 'System',
      deleteVersion: 'Delete',
      deleteVersionTitle: 'Are you sure to delete version {{version}}?',
      deleteVersionDescription:
        'This version is removed from the history permanently. The current version, the parser baseline and the record of a removal are kept.',
      versionDeleted: 'Version deleted',
      versionNotDeletable: 'This version cannot be deleted',
      versionNotDeletableDescription:
        'The version is still needed: the current state, the parser baseline, the record of a removal, and any state a dataset version restores from are kept.',
      tags: 'Tags',
      versionHistoryTip: 'Pick a version to preview what it stored.',
      versionChanges: 'Changes',
      // The pane beside Changes shows the whole state a version stored - body,
      // keywords, questions, tags, visibility - so it is "Details", not "Content",
      // which would only promise the body.
      versionContent: 'Details',
      // Where a version came from: the side that made the change, or - for a
      // restore - the state it replayed. A replay names nobody, so the actor alone
      // cannot tell the reader whether a change was theirs.
      detailOrigin: 'Origin',
      originChunk: 'Manual change',
      // A row can also record that the chunk appeared or disappeared, so naming
      // the side alone would not say which of the three happened here. All three
      // read the same way, and the "by ..." lines below name the sides a person
      // did not act on.
      originAdd: 'Manual addition',
      originRemove: 'Manual removal',
      // A re-parse drops the chunks it no longer produces: that removal belongs
      // to the pipeline, not to the page.
      originRemoveParse: 'Removed by a re-parse',
      // A replay left the chunk absent: the row records a removal, and the
      // dataset version it belongs to says where the removal came from.
      originRestore: 'Removal (dataset version: {{dataset}})',
      originRestorePlain: 'Removal',
      originParse: 'Written by document parsing',
      compareFrom: 'From',
      compareTip: 'Pick two versions to see what changed.',
      noDiff: 'These versions have no differences.',
      versionMissing:
        'This version does not exist. The chunk may never have been edited, or a re-parse dropped its history.',
      diffChanged: 'Changed',
      diffFieldContent: 'Content',
      diffFieldType: 'Chunk type',
      diffFieldImage: 'Image',
      diffFieldVisibility: 'Visibility',
      // A version that recorded no picture, shown in its half of the comparison.
      noImage: 'No image',
      // The card's detail popover: what the row is, when it appeared and when it
      // last changed. The times are read from the version history, because a
      // version row is the only record of a chunk's instants.
      detail: 'Details',
      detailTitle: 'Chunk details',
      detailId: 'Chunk ID',
      detailCreatedAt: 'Created',
      detailUpdatedAt: 'Updated',
      // Also the line the preview header uses: the version a row holds, since
      // the list beside it carries only an instant and a user.
      detailVersion: 'Version',
      detailStatus: 'Status',
    },
    datasetVersion: {
      current: 'Current',
      noVersions:
        'No versions yet. Change the configuration to start its history.',
      restoredFrom: 'Restored from v{{version}}',
      summaryTitle: 'Summary:',
      // Plural keys: the count picks the form, so a one-file version reads
      // "1 file" instead of "1 file(s)".
      counts_one: '{{count}} file',
      counts_other: '{{count}} files',
      documentsTitle: 'Files:',
      documentsShowAll: 'Show {{count}} more',
      documentsShowLess: 'Show fewer',
      changesShowAll: 'Show {{count}} more',
      changesShowLess: 'Show fewer',
      columnVersion: 'Version',
      columnKind: 'Change',
      columnTime: 'Time',
      columnCreatedBy: 'Created by',
      systemActor: 'System',
      versionMissing:
        'This version does not exist. The history may have been rebuilt.',
      // The action bar already sits under the version list and acts on the row the
      // list highlights, and the dialogs behind both buttons name the version, so
      // the buttons do not repeat it - the chunk version history uses the same two
      // short labels.
      restore: 'Restore',
      // Only shown as the disabled restore button's title: the panel does not
      // carry an explanatory sentence about what a restore does.
      restoreCurrentTip: 'This is already the current version.',
      restoreBlockedTitle: 'Cannot restore this version',
      restoreConfirmTitle: 'Restore to version {{version}}?',
      // A plan moves documents in two directions and one of them is usually
      // empty, so the sentence names only what actually moves: "restores 0
      // documents" reads like a failure, yet it is the ordinary case of undoing
      // an upload.
      restoreConfirmBoth:
        'This restores {{restored}} documents and removes {{deleted}} documents that were uploaded after this version',
      restoreConfirmRestoreOnly:
        'This restores {{restored}} documents that were deleted after this version',
      restoreConfirmDeleteOnly:
        'This removes {{deleted}} documents that were uploaded after this version',
      restoreIncompleteTitle: '{{count}} documents could not be fully restored',
      // The body heads the list the notice shows under it: one document per line,
      // each with the reason the server gave for that document.
      restoreIncompleteBody:
        'While the document rows are back, their content is not - parse them again:',
      restoreIncompleteMore: 'and {{count}} more not listed here',
      // Each reason follows the document it belongs to, so it names one document.
      restoreMissingNoChunkHistory: 'no chunk history reaches this version',
      restoreMissingRemovalGone: 'the record of its removal was cleaned up',
      restoreMissingReplayFailed: 'replaying its chunks failed',
      restoreMissingHistoryUnreadable: 'its chunk history could not be read',
      restored: 'Restored to this version',
      delete: 'Delete',
      deleteTitle: 'Are you sure to delete version {{version}}?',
      deleted: 'Version deleted',
      kindBaseline: 'Initial',
      kindDatasetConfig: 'Dataset config changed',
      kindDocumentConfig: 'Document config changed',
      kindDocumentRename: 'Document renamed',
      kindDocumentMetadata: 'Document metadata changed',
      kindDocumentStatus: 'Document enabled or disabled',
      kindDocumentEnable: 'Document enabled',
      kindDocumentDisable: 'Document disabled',
      kindArtifactEdit: 'Wiki page edited',
      kindRestore: 'Restore',
      kindUpload: 'Documents added',
      kindDelete: 'Documents removed',
      kindParse: 'Document parsed',
      kindChunk: 'Chunk changed',
      // The chunk kinds name the action the version recorded. `kindChunk` above
      // stays for rows written before these existed and for a mixed batch.
      kindChunkAdd: 'Chunk added',
      kindChunkUpdate: 'Chunk edited',
      kindChunkDelete: 'Chunk removed',
      kindChunkRestore: 'Chunk restored',
      kindChunkEnable: 'Chunk enabled',
      kindChunkDisable: 'Chunk disabled',
      // Full-page history view.
      pageTitle: 'Versions',
      pageDescription:
        'Every change this dataset went through, including configuration, document, chunk and wiki page changes.',
      filterAll: 'All changes',
      filterConfiguration: 'Configuration only',
      filterDocuments: 'Documents only',
      // A compiled wiki page is an artifact rather than a document, so it gets a
      // bucket of its own instead of inflating the document list.
      filterPages: 'Wiki pages only',
      filterChunks: 'Chunks only',
      changesTitle: 'Changes:',
      changesConfiguration: 'Changed dataset configuration',
      changesDocumentConfiguration: 'Changed document configuration',
      changesDocumentConfigurationFor:
        'Changed document configuration for {{documents}}',
      configChangeLine: '{{field}}: {{before}} → {{after}}',
      configAvatarSet: 'set',
      configAvatarEmpty: 'none',
      configParseModeBuiltin: 'Built-in',
      configParseModePipeline: 'Pipeline',
      configPipelineWithAlias: '{{pipeline}} (alias: {{alias}})',
      configPipelineLatest: '{{pipeline}} (latest version)',
      changesEmpty: 'This version recorded no change detail.',
      // A change reads as one sentence ("Added document handbook.pdf"), so the
      // entity is a common noun and the order is a sentence's, not a table's.
      changesLine: '{{action}} {{entity}} {{name}}',
      changesRenameLine: '{{action}} {{entity}} {{before}} to {{after}}',
      changesMetadataLine: '{{action}} metadata for {{name}}',
      // The writer stores a bounded set and keeps the true count beside it: a
      // shorter list is a sample, and the reader has to be told. "Recorded", not
      // "listed": the fold above is the panel's own preview of the stored set,
      // while this sentence is about the stored set itself being partial.
      changesTruncated:
        'Only the first {{shown}} of {{total}} changes were recorded.',
      chunkChangeLine: '{{action}} chunk {{chunk}} of document {{document}}',
      chunkVersionTag: '(chunk version: v{{version}})',
      changeEntityDocument: 'document',
      changeEntityChunk: 'chunk',
      changeEntityArtifact: 'wiki page',
      changeActionAdd: 'Added',
      changeActionUpdate: 'Changed',
      changeActionChunkEdit: 'Edited',
      changeActionRename: 'Renamed',
      changeActionDelete: 'Removed',
      changeActionParse: 'Parsed',
      changeActionRestore: 'Restored',
      changeActionEnable: 'Enabled',
      changeActionDisable: 'Disabled',
      field: {
        name: 'Name',
        avatar: 'Avatar',
        description: 'Description',
        language: 'Language',
        embd_id: 'Embedding model',
        parse_type: 'Parse type',
        parser_id: 'Chunk method',
        pipeline_id: 'Data pipeline',
        pipeline_version_id: 'Pipeline version',
        pagerank: 'PageRank',
        similarity_threshold: 'Similarity threshold',
        vector_similarity_weight: 'Vector similarity weight',
        permission: 'Permission',
        parser_config: 'Parser configuration',
        connectors: 'Data sources',
      },
    },
    knowledgeDetails: {
      syncLog: 'Sync log',
      summary: 'Summary',
      updated: 'Updated',
      versions: 'Versions',
      metadata: {
        // Shown by the document metadata editor on resume-parsed documents:
        // the extractor rewrites these field names on every re-parse.
        parserManagedFields:
          'These fields are maintained by the Resume parser; re-parsing overwrites or clears hand-filled values: ',
      },
    },
    common: {
      view: 'View',
      duplicate: 'Duplicate',
      duplicateSuffix: '(copy)',
      sync: 'Sync',
      all: 'All',
    },
    login: {
      signInWith: 'Sign in with {{name}}',
      feishuQrTitle: 'Scan the QR code with your Feishu app',
      feishuQrFailed:
        'Failed to load the QR code. Use the Feishu icon below to sign in instead.',
      wecomQrTitle: 'Scan the QR code with your WeCom app',
      wecomQrFailed:
        'Failed to load the QR code. Use the WeCom icon below to sign in instead.',
      dingtalkQrTitle: 'Scan the QR code with your DingTalk app',
      dingtalkQrFailed:
        'Failed to load the QR code. Use the DingTalk icon below to sign in instead.',
      usernameLabel: 'Username',
      usernamePlaceholder: 'Please input username',
      ldapServerLabel: 'LDAP server',
      ldapServerPlaceholder: 'Please select LDAP server',
      licenseExpired:
        'License expired. Please contact your administrator to renew the license.',
      confirmPasswordError: 'Password and confirm password must be the same.',
      captchaPlaceholder: 'Please input captcha',
      back: 'Back',
      confirmPasswordPlaceholder: 'Please input confirm password',
      confirmPassword: 'Confirm password',
      resetPassword: 'Reset password',
      sendTip: 'We will email you a verification code to reset your password.',
      verifyCodeTip:
        "We sent a reset link to <span style='color: rgb(var(--accent-primary));font-style: italic;'>{{email}}</span>\n                      enter 4 digit code that mentioned in the email",
      send: 'Send',
      resendEmail: 'Resend email',
      notGotEmail: "Haven't got the email yet?",
      forgetPassword: 'Forget password?',
      forgetPasswordDescription:
        'Please input your email to reset your password.',
      verifyCode: 'Verification code',
      verificationCode: 'Verification code',
      verifyCodePlaceholder: 'Verification code',
      checkEmail: 'Check email',
      registerVerifyCodeTip:
        "We sent a verification code to <span style='color: rgb(var(--accent-primary));font-style: italic;'>{{email}}</span>. Please enter the code to verify your email address.",
    },
    header: {
      tickets: 'Support',
    },
    tickets: {
      title: 'Tickets',
      createTicket: 'Create Ticket',
      state: 'State',
      priority: 'Priority',
      group: 'Group',
      customer: 'Customer',
      createdAt: 'Created At',
      viewConversation: 'View conversation',
      closeTitle: 'Close Ticket',
      closeConfirmMessage: 'Has your problem been resolved?',
      myTicket: 'My Ticket',
      states: {
        new: 'New',
        open: 'Open',
        pendingClose: 'Pending Close',
        pendingReminder: 'Pending Reminder',
        closed: 'Closed',
      },
      priorities: {
        low: 'Low',
        normal: 'Medium',
        high: 'High',
      },
      detail: {
        subject: 'Subject',
        group: 'Group',
        priority: 'Priority',
        state: 'State',
        updatedAt: 'Updated At',
        replyLabel: 'Reply',
        replyPlaceholder: 'Enter your reply...',
        sendReply: 'Send',
        closedNotice: 'This ticket has been closed.',
        dragDropHint: 'Drag & drop files here',
        uploadLimit: 'Upload max {{count}} files, each up to {{size}}',
      },
      form: {
        title: 'Title',
        titlePlaceholder: 'Please input ticket title',
        group: 'Group',
        customerName: 'Customer Name',
        customerNamePlaceholder: 'Please input customer name',
        customer: 'Customer Email',
        customerPlaceholder: 'Please input customer email',
        articleSubject: 'Article Subject',
        articleSubjectPlaceholder: 'Please input subject',
        articleBody: 'Article Body',
        articleBodyPlaceholder: 'Please input ticket description',
        attachments: 'Attachments',
        uploadAttachment: 'Click or drag files to upload',
      },
    },
    knowledgeList: {
      duplicatingTip: 'Duplicating...',
      duplicateModal: {
        title: 'Duplicate dataset',
        description: 'Create a copy with a new name',
      },
    },
    knowledgeConfiguration: {
      parserOptionUnavailableMessage:
        'The selected parser or pipeline version is no longer available. Please select again.',
      switchEmbeddingModel: {
        title: 'Switch embedding model',
        description:
          '<p>Different embedding models use different vector spaces.</p>\n<p>Switching the model will:</p>\n<ol><li>trigger a full re-processing</li><li>re-embedding of all existing files.</li></ol>\n<p>This may take some time depending on the size of your dataset.</p>',
      },
    },
    setting: {
      feishu_wikiDescription:
        'Sync file attachments, native document bodies, spreadsheets and bitables (converted to Markdown) from a Feishu or Lark Wiki subtree.',
      dataSourceFieldFeishuPlatform: 'Platform',
      dataSourceFieldFeishuAppId: 'Feishu app ID',
      dataSourceFieldFeishuAppSecret: 'Feishu app secret',
      dataSourceFieldWikiSpaceId: 'Wiki space ID',
      dataSourceFieldRootNodeToken: 'Wiki root node token',
      dataSourceFieldIncludeExtensions: 'Allowed file extensions',
      dataSourceFieldIncludeKeywords: 'Required filename keywords',
      dataSourceFieldExcludeKeywords: 'Excluded filename keywords',
      dataSourceFieldMaxFileSizeBytes: 'Maximum file size (bytes)',
      dataSourceValidationFeishuBatchSize:
        'Batch size must be between 1 and 10',
      dingtalkDescription:
        'Connect DingTalk to sync AI Table records or knowledge base and drive spaces.',
      dataSourceFieldDingTalkType: 'Type',
      dataSourceFieldDingTalkAppKey: 'DingTalk app key',
      dataSourceFieldDingTalkAppSecret: 'DingTalk app secret',
      dataSourceFieldDingTalkDriveMode: 'Drive',
      dataSourceFieldDingTalkDriveSpaceId: 'Drive space ID',
      deleteAccount: 'Delete account',
      deleteAccountDescription:
        'Permanently delete your account and associated data. This cannot be undone.',
      deleteAccountWarning:
        'This removes your owned workspace and its resources, and datasets and documents you created in other workspaces. Transfer ownership or remove other workspace members first.',
      deleteAccountConfirmation:
        'Enter your account email ({{email}}) to confirm.',

      openRouterProviderOrderTip:
        'Comma-separated provider list, e.g. Groq,Fireworks',
      requestAdminAddModel:
        'Please contact the administrator to add the model.',
      billing: 'Billing',
      modelGroup: 'Model Group',
      modelGroupPage: {
        title: 'Model groups',
        newGroup: 'New model group',
        createTitle: 'Create model group',
        groupName: 'Group name',
        nameRequired: 'Enter a group name',
        nameDuplicate: 'This group name already exists',
        empty: 'No model groups yet',
        loading: 'Loading',
        loadFailed: 'Failed to load model groups',
        membersLoadFailed: 'Failed to load group members',
        retry: 'Retry',
        addMember: 'Add model',
        addMemberTitle: 'Add model to group',
        memberAlreadyAdded: 'This model is already in the group',
        memberUnavailable: 'Unavailable',
        memberNoAccess: 'No access',
        removeMemberTitle: 'Remove model from group?',
        deleteGroupTitle: 'Delete model group?',
        deleteGroupDescription:
          'Only the group is deleted. Provider models are not affected.',
        providerName: 'Provider',
        modelName: 'Model name',
        modelType: 'Model type',
        instanceName: 'Instance name',
        position: 'Position',
        status: 'Status',
        reorderModel: 'Drag to reorder',
      },
      modelGroupSelector: {
        direct: 'Model',
        group: 'Model Group',
        unavailable: 'Unavailable',
        pending: 'Model groups are still loading. Wait for loading to finish.',
      },
    },
    message: {
      deleteIncomplete:
        'Could not confirm complete deletion. Check the remaining documents before retrying.',
      duplicated: 'Duplicated successfully',
    },
    fileManager: {
      syncFile: 'Sync file',
      syncFiles: 'Sync files',
      importData: 'Import data',
      dataSource: 'Data source',
      targetFolder: 'Target folder',
      syncFileSuccess: 'Sync file successfully',
      evaluation: {
        export: 'Export',
        query: 'Query',
        answer: 'Answer',
        evaluation: 'Evaluation',
        dataId: 'Data ID',
        createdAt: 'Created time',
        editName: 'Edit name',
        view: 'View',
        delete: 'Delete',
        deleteSelected: 'Delete selected',
        noEvaluationData: 'No evaluation data',
        loading: 'Loading...',
      },
    },
    flow: {
      removeLowContrastWordText: 'Ignore nearly invisible text in Word',
      removeLowContrastWordTextDescription:
        'Skip text that is too close to the background color when parsing DOC and DOCX.',
      cADChunkerDescription:
        'Split CAD drawings by sheet and spatial layout, with optional vision-model descriptions of drawing regions.',
      cadParserNoConfig:
        'CAD drawings are processed by the built-in parser and require no configuration. Configure vision enhancement in CAD Chunker.',
      visioParserNoConfig:
        'The built-in parser processes .vsdx, .vsdm, and Visio v11 .vsd drawings page by page without extra configuration.',
      cadVisionEnhancementTip:
        'Use a vision model to describe geometry, symbols, and spatial relationships in drawing regions; when off, only native CAD text is used.',
      extractorResumeTitle: 'Resume extraction',
      extractorResumeDescription:
        'Runs four LLM lanes (basic info, work experience, education, projects) over the text and writes structured metadata, keywords, and the embedding summary. Names, employers, positions, schools, degrees, and tenure are checked against the source text before they are stored. Re-parsing overwrites the managed metadata fields; the other extraction groups are not needed for resumes.',
      fileFormatOptions: {
        cad: 'CAD Drawing',
        visio: 'Visio Drawing',
      },
      version: {
        noPublished: 'No published versions',
        addAlias: 'Add alias',
        editAlias: 'Edit alias',
        editAliasDescription:
          'Give this version a memorable alias so you can find it quickly later.',
      },
    },
    deleteModal: {
      delDepartment: 'Delete department',
      delGroup: 'Delete group',
    },
    evaluation: {
      title: 'Evaluation',
      newEvaluation: 'New evaluation',
      caseId: 'Case ID',
      question: 'Question',
      referenceAnswer: 'Reference answer',
      modelAnswer: 'Model answer',
      score: 'Score',
      status: 'Status',
      actions: 'Actions',
      viewDetails: 'View Details',
      rerun: 'Rerun',
      clear: 'Clear',
      avgScore: 'Avg Score',
      relevance: 'Relevance',
      accuracy: 'Accuracy',
      fluency: 'Fluency',
      relevancy: 'Relevancy',
      factuality: 'Factuality',
      consistency: 'Consistency',
      contextRelevanceTip:
        'Measures the degree of correlation between the generated answer and the original query, without assessing its factual accuracy.',
      faithfulnessTip:
        'Evaluates the factual consistency between the answer and the reference answer, including factual completeness.',
      semanticSimilarityTip:
        "Uses prompt-based LLM evaluation to measure whether the overall meaning and logic of the output align with the user's answer. Since it relies entirely on LLM scoring, the prompt is editable for customization.",
      contextRelevance: 'Relevancy',
      faithfulness: 'Factuality',
      semanticSimilarity: 'Consistency',
      all: 'All',
      metricsDetail: 'Evaluation Metrics Detail',
      metrics: 'Metrics',
      additionalInfo: 'Additional Information',
      executionTime: 'Execution Time',
      tokenUsage: 'Token Usage',
      settings: 'Evaluation Settings',
      evaluationData: 'Evaluation data',
      evaluationMetrics: 'Evaluation metrics',
      selectCollection: 'Select evaluation data',
      selectModel: 'Select model',
      prompt: 'Prompt',
      promptPlaceholder: 'input',
      defaultModel: 'default model',
      errors: {
        collectionRequired: 'Please select a collection',
      },
      notConfigured: 'Not configured',
      failed: 'Run failed',
      completed: 'Run finished',
      configuration: 'Configuration',
    },
    admin: {
      workflow: 'Workflow',
      pipeline: 'Pipeline',
      assetLoadFailed: 'Failed to load assets',
      assetColumns: {
        team: 'Team',
        documentNum: 'Document Num',
        fileSize: 'File Size',
        chunkNum: 'Chunk Num',
        createAt: 'Create at',
        lastSaveAt: 'Last save at',
      },
      resourceType: {
        model_provider: 'Model provider',
        model_group: 'Model group',
        data_source: 'Data source',
        chat_channel: 'Chat channel',
        group: 'Group',
        mcp: 'MCP',
        invitation: 'Invitations',
      },
      permissionType: {
        accessControl: 'Access control',
      },
      settings: 'Settings',
      ssoProvider: 'SSO provider',
      ssoProviderSubtitle:
        'Choose one identity source for your organization: Cloud IDP, LDAP, or CAS.',
      ssoProviderRadioGroup: {
        label: 'Provider',
        values: {
          none: 'None',
          cloud_idp: 'Cloud IDP',
          ldap: 'LDAP',
          cas: 'CAS',
        },
      },
      ssoProviderTabs: {
        cas: {
          addDialogTitle: 'Add CAS',
          dialogTitle: 'Configure {{name}}',
          fields: {
            name: 'Display name',
            protocolVersion: 'CAS version',
            loginUrl: 'Login URL',
            validationUrl: 'Ticket validation URL',
            redirectUri: 'Redirect URI',
            emailAttribute: 'Email attribute',
            nicknameAttribute: 'Display name attribute',
          },
          description: {
            emailFromUsername:
              'RAGFlow requires an email address as the CAS 1.0 username.',
            emailAttribute:
              'Leave blank if the CAS username is an email address.',
            nicknameAttribute: 'Leave blank to use the CAS username.',
            redirectUri:
              'Register this URL as the login callback in your CAS provider.',
          },
        },
        cloud_idp: {
          title: 'Cloud identity providers',
          github: {
            title: 'GitHub',
            dialogTitle: 'Configure GitHub SSO',
            loginTip: 'Login with GitHub',
            fields: {
              clientId: 'Client ID',
              clientSecret: 'Client secret',
              url: 'URL',
            },
          },
          google: {
            title: 'Google',
            dialogTitle: 'Configure Google SSO',
            loginTip: 'Login with Google',
            fields: {
              clientId: 'Client ID',
              clientSecret: 'Client secret',
              redirectUri: 'Redirect URI',
            },
            placeholder: {
              redirectUri: 'http://<domain>/api/v1/auth/oauth/google/callback',
            },
          },
          feishu: {
            title: 'Feishu',
            dialogTitle: 'Configure Feishu SSO',
            loginTip: 'Login with Feishu',
            fields: {
              clientId: 'App ID',
              clientSecret: 'App secret',
              redirectUri: 'Redirect URI (optional)',
            },
            placeholder: {
              redirectUri: 'http://<domain>/api/v1/auth/oauth/feishu/callback',
            },
            description: {
              redirectUri:
                'Leave empty to use the single redirect URL registered with the Feishu app.',
            },
          },
          wecom: {
            title: 'WeCom',
            dialogTitle: 'Configure WeCom SSO',
            loginTip: 'Login with WeCom',
            fields: {
              clientId: 'Corp ID',
              agentId: 'Agent ID',
              clientSecret: 'App secret',
              redirectUri: 'Redirect URI',
            },
            placeholder: {
              redirectUri: 'http://<domain>/api/v1/auth/oauth/wecom/callback',
            },
            description: {
              clientId:
                'The "Corp ID" shown in the WeCom admin console under My Company.',
              agentId:
                'The "AgentId" of the self-built application used for login.',
              redirectUri:
                'Members are handed the code here after scanning. Register the same URL as the trusted callback domain of the application, and give the application the contact permission so login can read the biz_mail.',
            },
          },
          dingtalk: {
            title: 'DingTalk',
            dialogTitle: 'Configure DingTalk SSO',
            loginTip: 'Login with DingTalk',
            fields: {
              clientId: 'AppKey (client_id)',
              clientSecret: 'AppSecret (client_secret)',
              redirectUri: 'Redirect URI',
            },
            placeholder: {
              redirectUri:
                'http://<domain>/api/v1/auth/oauth/dingtalk/callback',
            },
            description: {
              clientId:
                'The "AppKey" of the enterprise internal application used for login.',
              redirectUri:
                "Members are handed the authCode here after scanning. The page rendering the QR must share this URL's origin, and the application needs the contact personal-information read permission so login can read the email.",
            },
          },
        },
        ldap: {
          title: 'Enterprise directory (LDAP)',
          addDialogTitle: 'Add LDAP',
          editDialogTitle: 'Configure LDAP',
          fields: {
            name: 'Name',
            url: 'URL',
            dn: 'DN (Distinguished name)',
            password: 'Password',
            searchFilter: 'Search filter',
            attributeList: 'Attribute list',
          },
          placeholder: {
            name: 'Server name',
            url: 'ldaps://ad01.mycompany.com:636',
            dn: 'CN=svc_ldap_app, OU=Service...',
          },
        },
      },
      settingsSubtitle: 'Manage system-wide configurations.',
      changeOwnPassword: {
        failed: 'Could not change the password. Please try again.',
        encryptionFailed:
          'Could not encrypt the passwords. Try a shorter password.',
      },
      settingsForm: {
        apiKey: {
          title: 'API keys',
          description:
            'Set the maximum number of API keys per tenant. Existing keys remain valid when the limit is lowered.',
          maxCount: 'Maximum keys per tenant',
        },
        smtp: {
          title: 'SMTP',
          description:
            'Configure the system email sender and connection details.',
          testConnection: 'Test connection',
          testConnectionMessageSuccess: 'Test connection successful',
          testConnectionMessageError: 'Test connection failed',
          fields: {
            server: 'Server',
            port: 'Port',
            timeout: 'Timeout (seconds)',
            username: 'Username',
            password: 'Password',
            defaultSender: 'Default sender',
            ssl: 'SSL',
            tls: 'TLS',
          },
          placeholder: {
            server: 'smtp.example.com',
            username: 'your-email@example.com',
            defaultSender: 'noreply@example.com',
          },
          messages: {
            server: 'Please enter the server address',
            port: 'Port number must be an integer and must be between 1 and 65535',
            timeout: 'Please enter the timeout between 0 and 300',
            username: 'Please enter the username',
            password: 'Please enter the password',
            defaultSender: 'Please enter the default sender',
          },
        },
        whitelist: {
          title: 'Registration whitelist',
          description: 'Configure system whitelist who can register RAGFlow.',
          fields: {
            enableModule: 'Enable module',
          },
        },
        notification: {
          title: 'Notification',
          description: 'Manage notification content and delivery settings.',
          fields: {
            content: 'Content',
            enableModule: 'Enable',
          },
        },
        sensitiveWordFilter: {
          title: 'Sensitive Word Filtering',
          description:
            'Block questions containing configured sensitive words across chat, search, and agent chat.',
          fields: {
            enableModule: 'Enable',
            response: 'Response',
            file: 'Sensitive words file',
          },
          placeholder: {
            response:
              'Your request could not be processed because it contains restricted content. Please modify your input and try again.',
          },
          fileTip:
            'Upload one .txt file up to 30 MiB. Use one sensitive word or phrase per line.',
          download: 'Download current file',
          downloadFailed: 'Sensitive words file is not available',
          uploaded:
            'Sensitive words file "{{filename}}" uploaded. {{count}} words loaded.',
          messages: {
            response: 'Please enter the response',
            fileType: 'Only .txt files are allowed',
            fileSize: 'The file must be 30 MiB or smaller',
          },
        },
        exposeModelProvider: {
          title: 'Expose Model Provider',
          description: 'Display the model provider page to non-admin users.',
          fields: {
            enableModule: 'Enable',
          },
        },
        exposeApi: {
          title: 'Expose API',
          description: 'Display the API page to non-admin users.',
          fields: {
            enableModule: 'Enable',
          },
        },
        emailVerification: {
          title: 'Email Verification',
          description:
            "Send verification code to the user's registered email via the system default mailbox.",
          fields: {
            enableModule: 'Enable',
          },
        },
      },
      ldapServer: 'LDAP server',
      signInWith: 'Sign in with {{name}}',
      plan: 'Plan',
      trial: 'Trial',
      starter: 'Starter',
      pro: 'Pro',
      modelProviders: 'Model providers',
      tenants: 'Tenants',
      defaultTenantRole: 'Default tenant role',
      setAsDefaultTenantRole: 'Set as default',
      defaultTenantRoleUpdated: 'Default tenant role updated.',
      defaultTenantRoleLoadFailed: 'Could not load the default tenant role.',
      defaultTenantRoleUpdateFailed:
        'Could not update the default tenant role.',
      tenantName: 'Tenant',
      tenantOwner: 'Owner',
      tenantMemberCount: 'Members',
      tenantRole: 'Tenant role',
      tenantRoleUpdated: 'Tenant role updated.',
      tenantRoleUpdateFailed: 'Could not update the tenant role.',
      tenantStatusUpdated: 'Tenant status updated.',
      tenantStatusUpdateFailed: 'Could not update the tenant status.',
      searchTenants: 'Search tenants',
      tenantsLoadFailed: 'Could not load tenants.',

      view: 'View',
      selectTimeRange: 'Select time range',
      export: 'Export',
      registrationWhitelistNotEnabled: 'Registration whitelist is not enabled',
      modelUsage: {
        modelUsageStatistics: 'Model usage',
        modelUsageSubtitle: 'Token consumption across users and models.',
        totalTokens: 'Total tokens',
        totalRequests: 'Total requests',
        successRate: 'Success rate',
        rankList: 'Rank list',
        rank: 'Rank',
        email: 'Email',
        tokens: 'Tokens',
        requests: 'Requests',
        usageDetail: 'Usage detail',
        statDate: 'Stat date',
        modelName: 'Model name',
        providerName: 'Provider name',
        userEmailPlaceholder: 'Enter user email',
        selectUserHint: 'Enter a user email to inspect their usage.',
        topN: 'Top {{count}}',
        today: 'Today',
        yesterday: 'Yesterday',
        last7Days: 'Last 7 days',
        last30Days: 'Last 30 days',
        last90Days: 'Last 90 days',
        thisMonth: 'This month',
        lastMonth: 'Last month',
        hour: 'Hour',
        day: 'Day',
        month: 'Month',
      },
      ingestionTasks: {
        title: 'Ingestion tasks',
        document: 'Document',
        dataset: 'Dataset',
        user: 'User',
        ingestion: 'Ingestion',
        createAt: 'Create at',
        updateAt: 'Update at',
        statusLabel: 'Status',
        action: 'Action',
        viewLog: 'View log',
        logTitle: 'Run log',
        allStatuses: 'All statuses',
        loadFailed: 'Unable to load ingestion tasks.',
        status: {
          CREATED: 'Created',
          SCHEDULED: 'Scheduled',
          RUNNING: 'Running',
          STOPPING: 'Stopping',
          COMPLETED: 'Completed',
          FAILED: 'Failed',
          STOPPED: 'Stopped',
        },
      },
    },
    license: {
      title: 'License',
      description: 'Manage your licenses and ensure continuous operation.',
      addLicense: 'Add license',
      table: {
        id: 'ID',
        startTime: 'Start time',
        expiryTime: 'Expiry time',
        status: 'Status',
      },
      status: {
        valid: 'Valid',
        expired: 'Expired',
        pending: 'Pending',
      },
    },
    permission: {
      myTeam: 'My team',
      member: 'Member',
      group: 'Group',
      expand: 'Expand',
      collapse: 'Collapse',
      role: 'Role',
      createRole: 'Create role',
      editRole: 'Edit role',
      rolePermissions: 'Role permissions',
      roleConfiguration: 'Role configuration',
      roleManagement: 'Role management',
      assets: 'Assets',
      integrations: 'Integrations',
      organization: 'Organization',
      modelProviders: 'Model providers',
      modelProvidersDescription:
        'Choose which model provider types members with this role can use.',
      dataSourcesDescription:
        'Choose which data source types members with this role can use.',
      chatChannelsDescription:
        'Choose which chat channel types members with this role can use.',
      roleConfigurationDenied: 'You do not have permission to manage roles.',
      roleConfigurationLoadFailed: 'Could not load role configuration.',
      roleDefaultModels: 'Default models',
      roleDefaultModelsDescription:
        'Choose the models members with this role use by default.',
      roleDefaultModelsLoadFailed: 'Could not load role default models.',
      roleDefaultModelUpdated: 'Default model updated.',
      resource: 'Resource',
      members: 'Members',
      membersWithRole: '{{count}} members',
      membersWithRole_one: '{{count}} member',
      defaultRole: 'Default',
      roleCreated: 'Role created.',
      roleUpdated: 'Role updated.',
      roleDeleted: 'Role deleted.',
      defaultRoleUpdated: 'Default role updated.',
      rolePermissionsUpdated: 'Role permissions updated.',
      rolePermissionsLoadFailed: 'Could not load role permissions.',
      manageTenantPermissions: 'Manage tenant permissions',
      manage_members: 'Manage members',
      manage_roles: 'Manage roles',
      tenantPermissionsUpdated: 'Tenant permissions updated.',
      inviteByLink: 'Invite with a link',
      inviteLinkDescription:
        'Anyone with this link can request to join the tenant. An administrator must approve the request. It expires in 7 days.',
      inviteLink: 'Invitation link',
      createInviteLink: 'Create invite link',
      inviteLinkExpiresAt: 'Expires {{date}}',
      tenantInvitationTitle: 'Request to join a tenant',
      tenantInvitationAlreadyJoinedTitle: "You're already a member",
      tenantInvitationAlreadyJoined: 'You already belong to {{tenant}}.',
      tenantInvitationPendingReviewTitle: 'Request pending review',
      tenantInvitationPendingReview:
        'Your request to join {{tenant}} is awaiting review.',
      tenantInvitationMembershipLoadFailed:
        "We couldn't verify your membership. Reload the page and try again.",
      tenantInvitationInvitedBy: '{{inviter}} invited you to join this tenant.',
      tenantInvitationInvalid:
        'This invitation link is invalid or has expired.',
      tenantInvitationSignIn: 'Sign in to request access',
      tenantInvitationRegister: 'Create an account',
      tenantInvitationRequestJoin: 'Request to join',
      tenantInvitationRequestSubmitted:
        'Your request to join {{tenant}} has been submitted and is awaiting review.',
      tenantInvitationOpenTenant: 'Open tenant',
      tenantInvitationSwitchFailed:
        'Could not switch to the tenant. Try again.',
      deleteRole: 'Delete role',
      deleteRoleWarning:
        'Members assigned to this role will be moved to the default role. The default role cannot be deleted.',
      department: 'Department',
      teamManagement: 'Team Management',
      membersInTotal: 'members in total',
      createGroup: 'Create group',
      leaveTheTeam: 'Leave the team',
      leaveGroup: 'Leave group',
      leaveGroupHint:
        'Leaving removes you from this group and its child groups. Group data and resources remain. Transfer ownership of this group or any child group first.',
      leaveTenantHint:
        'Leaving marks you as left. Your tenant role, group memberships, permissions, and resources are retained. Administrators can see your status in the member list.',
      owner: 'Owner',
      left: 'Left team',
      moveDepartment: 'Move department',
      manageMember: 'Manage member',
      createSubDepartment: 'Create sub department',
      description: 'Description',
      descriptionPlaceholder: 'Please input a description',
      administrator: 'Administrator',
      setAsAdministrator: 'Set as Administrator',
      transferOwner: 'Transfer Owner',
      permissionManagement: 'Permission Management',
      permissionManagementDescription:
        'Members can only view knowledge bases within their visible scope and corresponding permissions. Owners can assign all permissions for all knowledge bases. Please proceed with caution!',
      manage: 'Manage',
      collaborator: 'Collaborator',
      team: 'Team',
      addCollaborator: 'Add collaborator',
      mixedPermission: 'Mixed',
      invisible: 'Invisible',
      read: 'Read',
      write: 'Write',
      manageCollaborator: 'Manage collaborator',
      permission: 'Permission',
      transferTo: 'Transfer to',
      avatar: 'Avatar',
      deleteConfirmMessage: '{{id}} is being used by {{type}} as follows',
      pleaseShareTheModelFirst:
        'Please share the following knowledge base or model first',
      type: 'Type',
      createSubgroup: 'Create subgroup',
      parentGroup: 'Parent group',
      rootGroup: 'Root group',
      groupNameLength: 'Group name must be at most 100 characters',
      addSubgroup: 'Add subgroup',
      deleteSubgroup: 'Delete subgroup',
      addMember: 'Add member',
      removeMember: 'Remove member',
      configureMemberPermissions: 'Configure member permissions',
      approveMember: 'Approve member request',
      memberApproved: 'Member request approved.',
      inviting: 'Inviting',
      pendingReview: 'Pending review',
      pending: 'Pending',
      memberPermissions: 'Member permissions',
      noInvitableMembers: 'No members available to add',
      teamInvitation: 'Team invitations',
      pendingInviteMembers:
        '{{count}} member(s) have not accepted the team invitation yet. They can join a group after accepting.',
      selectMember: 'Select member',
      transferOwnerHint:
        'The selected member will become the group owner and gain all group permissions.',
      switchTenant: 'Switch tenant',
    },
    privateLLM: {
      resetDefaultLLMTip: 'reset as default all users',
      resetDefaultLLMConfirm: 'Are you sure to reset default LLM?',
    },
    price: {
      free: 'Free',
      freeDesc:
        'Start for free and explore essential features to get your project off the ground.',
      starter: 'Starter',
      starterDesc:
        'Ideal for individuals and small teams starting their journey with essential features.',
      pro: 'Pro',
      proDesc:
        'Perfect for growing businesses requiring more advanced tools and higher limits.',
      enterprise: 'Enterprise',
      enterpriseDesc:
        'Enterprise-grade capabilities for production workloads at scale',
      upgrade: 'Upgrade',
      reduce: 'Reduce Now',
      upgradeNow: 'Upgrade now',
      contactUs: 'Contact Us',
      contactUsTip: 'Contact us for more information.',
      inUse: 'Cancel plan',
      paymentSuccessful: 'Payment successful',
      paymentSuccessfulTip:
        'Thank you! Your payment has been successfully processed.',
      paymentFailed: 'Payment failed',
      paymentFailedTip:
        'The transaction was not successful. Please verify your payment information or check your billing account. If this issue persists, contact us at support@ragflow.io with your order id for further assistance.',
      paymentVerificationRequired:
        'Payment requires additional verification. Complete verification to activate your plan.',
      paymentProcessing:
        'Payment is processing. Your plan will activate after payment is confirmed.',
      paymentDeclined:
        'Payment was declined. Use another payment method and try again.',
      paymentServiceUnavailable:
        'Payment service is temporarily unavailable. Please try again shortly.',
      duplicateSubscription:
        'Multiple active subscriptions were found. Please contact support before retrying payment.',
      cancelPlanTitle: 'Cancel {{plan}} Plan?',
      cancelPlanTip: 'You are canceling the {{plan}} Plan.',
      cancelPlanEffectiveTip:
        'After cancellation, you can continue using the current plan benefits until {{date}}, after which they will expire.',
      cancelPlanSwitchTip:
        'If your current usage exceeds the Trial plan limits, your downgrade will be blocked and you will remain on the current plan. Existing add-on storage does not extend the Trial storage quota, and any add-on storage will be cancelled when the current plan expires.',
      cancelPlanConfirmQuestion: 'Are you sure you want to cancel?',
      upgradeTips: {
        seatsLimit:
          "You've reached your team member count limit for your plan ({{current}}/{{limit}}). ",
        appsLimit:
          "You've reached your app count limit for your plan ({{current}}/{{limit}}). ",
        storageLimit:
          "You've reached your storage limit for your plan ({{current}}GB/{{limit}} GB). ",
        pointsInsufficient: 'Your points balance is insufficient. ',
        appsUpgrade: 'Upgrade to get more apps',
        storageUpgrade: 'Upgrade to get more storage',
        pointsUpgrade: 'Upgrade to get more points',
        teamMemberUpgrade: 'Upgrade to invite more',
      },
    },
    billing: {
      overview: 'Overview',
      usage: 'Usage',
      billingHistory: 'Billing History',
      managePaymentMethods: 'Manage payment methods',
      updatePaymentMethod: 'Update payment method',
      payInvoice: 'Pay invoice',
      subscription: {
        paymentRequired:
          'This subscription needs payment recovery before it can be changed or cancelled.',
      },
      needMore: 'Need more?',
      freePlan: 'Free Plan',
      starterPlan: 'Starter Plan',
      proPlan: 'Pro Plan',
      enterprisePlan: 'Enterprise Plan',
      billingCycle: 'Billing cycle',
      storage: 'Storage',
      apps: 'Apps',
      teamMember: 'Team Member',
      apiRequests: 'API Requests',
      payAsYouGo: 'Pay-As-You-Go',
      autoChargeInfo:
        'We auto-charge every time your running total reaches US$10. No spending limits—grow as you go.',
      documentParsing: 'Document Parsing',
      pages: 'pages',
      paid: 'Paid',
      unpaid: 'Unpaid',
      totalSpendThisCycle: 'Total spend this cycle',
      underTenCharged:
        'Any remaining amount under $10 will still be charged at the end of your billing cycle',
      manage: 'Manage',
      planUsed: 'used',
      addonUsed: 'Add-on used',
      currentMonthlyCost: 'Current monthly cost',
      nextMonthlyCost: 'Next monthly cost',
      reducedQuotaEffective: 'Reduced quota takes effect on',
      ensureBelow: 'Ensure usage is below',
      toAvoidOverage: 'to avoid overage',
      payNowIncremental:
        "Pay <strong class='text-text-primary'>{{amount}}</strong> now for the additional storage.<br>\n         Your next monthly storage cost will be <strong class='text-text-primary'>{{nextMonthlyCost}}</strong>.",
      manageAddonStorage: 'Manage Add-on storage',
      storageTitle: 'Storage',
      gb: 'GB',
      storageUpgradeSuccess:
        'Storage upgraded to {{value}}GB. Your payment has been processed.',
      storageUpgradeFailed:
        'Failed to apply the new storage target. Please try again.',
      today: 'Today',
      yesterday: 'Yesterday',
      last1Hour: 'Last 1 Hour',
      last6Hours: 'Last 6 Hours',
      last24Hours: 'Last 24 Hours',
      last7Days: 'Last 7 Days',
      last30Days: 'Last 30 Days',
      last90Days: 'Last 90 Days',
      last1Year: 'Last 1 Year',
      monthToDate: 'Month to Date',
      lastMonth: 'Last Month',
      yearToDate: 'Year to Date',
      lastYear: 'Last Year',
      pickDateRange: 'Pick a date range',
      showingVisitors: 'Showing total visitors for the last 7 days',
      totalSpend: 'Total Spend',
      spendCategories: 'Spend Categories',
      invoiceID: 'Invoice ID',
      subscriptionID: 'Subscription ID',
      plan: 'Plan',
      createDate: 'Created At',
      product: 'Product',
      products: 'Products',
      quantities: 'Quantities',
      amounts: 'Amounts',
      status: 'Status',
      amount: 'Amount',
      total: 'Total',
      invoice: 'Invoice',
      success: 'Success',
      pending: 'Pending',
      failed: 'Failed',
      currentPoints: 'Credit balance',
      equivalentUsd: 'Equivalent USD',
      points: 'pts',
      quickSelectTip: 'Quick select',
      customAmount: 'Custom amount',
      youWillReceive: 'You will receive',
      buyPointsCreating: 'Creating checkout...',
      buyPointsMinError: 'Please enter a valid amount',
      buyPointsFailed: 'Failed to create checkout session',
      buyPointsRequestFailed: 'Request failed, please try again',
      upgradePlan: 'Upgrade Now',
      changePlan: 'Change Plan',
      documentParse: 'Document parsing credits',
      creditsUsed: 'Credits used',
      buyCredits: 'Buy Credits',
      buyStorage: 'Buy Storage',
      paymentPending: 'Pending',
      paymentSuccess: 'Payment success',
      paymentFailed: 'Payment failed',
      planUpgradedSuccess: 'Plan upgraded successfully',
      storageAddedSuccess: 'Storage added successfully',
      pointsPurchasedSuccess: 'Points purchased successfully',
      viewInvoice: 'View Invoice',
      downloadPdf: 'Download PDF',
      viewReceipt: 'View Receipt',
      close: 'Close',
    },
    system: {
      uploadDescription:
        'The total file size limit per upload is {{size}}. Uploading prohibited files is strictly forbidden.',
    },
    visioPreview: {
      controlsLabel: 'Visio preview controls',
      previousPage: 'Previous page',
      nextPage: 'Next page',
      pageSelect: 'Visio page',
      pageFallback: 'Page {{index}}',
      zoomOut: 'Zoom out',
      zoomIn: 'Zoom in',
      fitToWindow: 'Fit to window',
      fit: 'Fit',
      targetPageMissing:
        'Unable to locate the referenced Visio page in this version; please select the page manually.',
      fileTooLarge: 'Visio preview supports files up to 32 MiB.',
      fetchFailed: 'Failed to fetch Visio document',
      loadFailed: 'Failed to load Visio file',
      renderFailed: 'Failed to render Visio page',
    },
    // Reasoning-engine levels 6-8 (see @/ee/hooks/use-thinking-modes).
    chat: {
      thinkingLevelAgentic: 'react',
      thinkingLevelAgenticDescription: 'Self-directed corpus exploration',
      thinkingLevelPlanExplorerAuditor: 'planer-explorer-auditor',
      thinkingLevelPlanExplorerAuditorDescription:
        'Plans a question decomposition, then explores it under a delivery gate',
      thinkingLevelIterative: 'IterSynth',
      thinkingLevelIterativeDescription:
        'Rewrites its research summary every round',
      thinkingLevelCodex: 'agentic pro',
      thinkingLevelCodexDescription:
        'Delegates to a shared Codex agent that reads the knowledge base',
    },
  },
};
