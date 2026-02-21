// =============================================================================
// Codex.app — 对话数据模型定义
// 从 ConversationManager 和通知处理逻辑中推导
//
// 这些是 React 状态中使用的核心数据结构
// =============================================================================

/**
 * @typedef {Object} Conversation
 * @property {string} id                             - Thread ID
 * @property {Turn[]} turns                          - 对话轮次列表
 * @property {ServerRequest[]} requests              - 待处理的审批请求
 * @property {number} createdAt                      - 创建时间 (ms)
 * @property {number} updatedAt                      - 更新时间 (ms)
 * @property {string|null} title                     - 对话标题 (AI 自动生成)
 * @property {string} latestModel                    - 当前使用模型
 * @property {string|null} latestReasoningEffort     - 推理力度 ("low"|"medium"|"high"|null)
 * @property {CollaborationMode|null} latestCollaborationMode
 * @property {boolean} hasUnreadTurn                 - 是否有未读轮次
 * @property {string} rolloutPath                    - Rollout 路径
 * @property {string} cwd                            - 工作目录
 * @property {GitInfo|null} gitInfo                  - Git 信息
 * @property {"resuming"|"resumed"|"needs_resume"} resumeState
 * @property {TokenUsageInfo|null} latestTokenUsageInfo
 * @property {string|null} source                    - 来源
 */

/**
 * @typedef {Object} Turn
 * @property {TurnParams} params                     - 发送时的参数
 * @property {string|null} turnId                    - 后端分配的 turn ID
 * @property {"inProgress"|"completed"|"cancelled"|"error"} status
 * @property {number} turnStartedAtMs                - 开始时间
 * @property {number|null} finalAssistantStartedAtMs - AI 开始回复时间
 * @property {Object|null} error                     - 错误信息
 * @property {Object|null} diff                      - 代码 diff
 * @property {TurnItem[]} items                      - 消息/工具/文件等
 */

/**
 * @typedef {Object} TurnParams
 * @property {string} threadId
 * @property {InputItem[]} input                     - 用户输入
 * @property {string|null} cwd
 * @property {string|null} approvalPolicy            - 审批策略
 * @property {string|null} sandboxPolicy             - 沙箱策略
 * @property {string|null} model
 * @property {string|null} effort
 * @property {string} summary
 * @property {string|null} personality
 * @property {Object|null} outputSchema
 * @property {CollaborationMode|null} collaborationMode
 * @property {Attachment[]} attachments
 */

// =============================================================================
// TurnItem 类型 — 对话轮次中的所有项目类型
// =============================================================================

/**
 * @typedef {AgentMessageItem|UserMessageItem|CommandExecutionItem|FileChangeItem|
 *           McpToolCallItem|ReasoningItem|PlanItem|PlanImplementationItem|
 *           TodoListItem|ErrorItem|ContextCompactionItem} TurnItem
 */

/**
 * AI 回复文本
 * @typedef {Object} AgentMessageItem
 * @property {string} id
 * @property {"agentMessage"} type
 * @property {string} text          - AI 回复内容 (流式累加)
 */

/**
 * 用户消息
 * @typedef {Object} UserMessageItem
 * @property {string} id
 * @property {"userMessage"} type
 * @property {InputItem[]} content  - [{type:"text",text:...}, {type:"image",...}]
 */

/**
 * 命令执行
 * @typedef {Object} CommandExecutionItem
 * @property {string} id
 * @property {"commandExecution"} type
 * @property {string} command                  - 执行的命令
 * @property {string|null} aggregatedOutput    - 聚合的输出文本
 * @property {number|null} exitCode
 * @property {string|null} cwd
 */

/**
 * 文件修改
 * @typedef {Object} FileChangeItem
 * @property {string} id
 * @property {"fileChange"} type
 * @property {string} path          - 文件路径
 * @property {string} action        - "create"|"edit"|"delete"
 * @property {string|null} diff     - unified diff
 */

/**
 * MCP 工具调用
 * @typedef {Object} McpToolCallItem
 * @property {string} id
 * @property {"mcpToolCall"} type
 * @property {string} serverName
 * @property {string} toolName
 * @property {Object} arguments
 * @property {Object|null} result
 */

/**
 * 推理过程 (Thinking)
 * @typedef {Object} ReasoningItem
 * @property {string} id
 * @property {"reasoning"} type
 * @property {string[]} summary     - 推理摘要 (按 index)
 * @property {string[]} content     - 推理内容 (按 index)
 */

/**
 * Plan 文本
 * @typedef {Object} PlanItem
 * @property {string} id
 * @property {"plan"} type
 * @property {string} text          - 计划文本 (流式累加)
 */

/**
 * Plan 实施项
 * @typedef {Object} PlanImplementationItem
 * @property {string} id
 * @property {"planImplementation"} type
 * @property {string} turnId
 * @property {string} planText
 * @property {boolean} isCompleted
 */

/**
 * Todo 列表 (来自 turn/plan/updated)
 * @typedef {Object} TodoListItem
 * @property {string} id
 * @property {"todo-list"} type
 * @property {string|null} explanation
 * @property {Object} plan          - 结构化计划
 */

/**
 * 错误信息
 * @typedef {Object} ErrorItem
 * @property {string} id
 * @property {"error"} type
 * @property {string} message
 * @property {boolean} willRetry
 * @property {Object|null} errorInfo
 * @property {string|null} additionalDetails
 */

/**
 * 上下文压缩
 * @typedef {Object} ContextCompactionItem
 * @property {string} id
 * @property {"contextCompaction"} type
 * @property {boolean} completed
 */

// =============================================================================
// 用户输入类型
// =============================================================================

/**
 * @typedef {TextInput|ImageInput|LocalImageInput|SkillInput|MentionInput} InputItem
 */

/** @typedef {{ type: "text", text: string }} TextInput */
/** @typedef {{ type: "image", url: string }} ImageInput */
/** @typedef {{ type: "localImage", path: string }} LocalImageInput */
/** @typedef {{ type: "skill", name: string, path: string }} SkillInput */
/** @typedef {{ type: "mention", name: string, path: string }} MentionInput */

// =============================================================================
// 其他类型
// =============================================================================

/**
 * @typedef {Object} CollaborationMode
 * @property {"default"|string} mode
 * @property {{ model: string, reasoning_effort: string|null, developer_instructions: string|null }} settings
 */

/**
 * @typedef {Object} GitInfo
 * @property {string|null} branch
 * @property {string|null} sha
 * @property {string|null} originUrl
 */

/**
 * @typedef {Object} TokenUsageInfo
 * @property {number} inputTokens
 * @property {number} outputTokens
 * @property {number} totalTokens
 */

/**
 * @typedef {Object} ServerRequest
 * @property {string} id
 * @property {string} method       - "item/commandExecution/requestApproval" | "item/fileChange/requestApproval" | "item/tool/requestUserInput"
 * @property {Object} params
 */

module.exports = {};
