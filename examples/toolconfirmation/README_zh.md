# 代码验证流程

## 步骤 1：运行代码

```bash
# 补充API密钥后运行
go run main.go
```

控制台会输出主菜单：

```plaintext
--- Menu ---
1: Chat with LLM
2: Manage Vacation Requests
3: Exit
Choose an option:
```

## 步骤 2：发起休假申请（通过 LLM）

1. 输入1进入「LLM Chat Mode」；
2. 输入休假申请指令（如我要申请5天休假，用户ID是user123）；
3. 观察输出：
    - LLM 会调用request_vacation_days工具，参数包含days:5、user_id:user123；
    - 工具返回「Manager approval is required.」，并生成请求 ID（如req-0）；
    - 输入back返回主菜单。

## 步骤 3：审批 / 拒绝休假请求

1. 输入2进入「Vacation Request Mode」；
2. 首先会展示待处理请求：

```plaintext
--- Pending Vacation Requests ---
ID: req-0, Call ID: xxx, User: user123, Days: 5, Status: PENDING, Days Approved: 0
-------------------------------
```

3. 审批请求：
    - 输入approve req-0；
    - 系统提示「How many days to approve for req-0 (requested 5)?」，输入3；
    - 观察输出：工具返回「The time off request is accepted.」，请求状态变为APPROVED，批准天数为 3；


4. （可选）拒绝请求：
    - 重新发起一个新的休假申请（如申请 4 天）；
    - 输入reject req-1；
    - 观察输出：工具返回「The time off request is rejected.」，请求状态变为REJECTED，批准天数为 0。

## 步骤 4：验证异常场景

1. 发起无效申请：在 LLM 聊天模式输入「我要申请 - 2 天休假」，工具会返回invalid days to request -2；
2. 审批不存在的请求：输入approve req-999，系统提示「Request ID req-999 not found or not pending.」；
3. 审批时输入无效天数：审批 req-0 时输入6（超过申请的 5 天），系统提示「Invalid number of days. Approval cancelled.」。

## 步骤 5：退出应用

输入3退出主菜单，验证应用正常终止。

## 总结

1. 代码核心是基于 ADK 实现带人工确认的 LLM 工具调用，模拟休假申请审批流程，核心逻辑在requestVacationDays函数中区分「首次发起」和「确认审批」两个阶段；
2. 验证流程需覆盖「正常发起申请→审批 / 拒绝→异常场景校验」，核心观察点是请求状态（PENDING/APPROVED/REJECTED）和返回结果的一致性；