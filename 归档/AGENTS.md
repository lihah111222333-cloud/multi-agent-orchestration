## Project Skills

- Local skill root: `.agnet/skills`
- Git worktree skill: `.agnet/skills/Git工作树/SKILL.md`
- PDF skill: `.agnet/skills/PDF文档处理/SKILL.md`
- Python skill: `.agnet/skills/Python专家/SKILL.md`
- Brainstorm skill: `.agnet/skills/头脑风暴/SKILL.md`
- Verify-before-complete skill: `.agnet/skills/完成前验证/SKILL.md`
- Parallel-agents skill: `.agnet/skills/并行代理调度/SKILL.md`
- Execute-plan skill: `.agnet/skills/执行计划/SKILL.md`
- TDD skill: `.agnet/skills/测试驱动开发/SKILL.md`
- Skill-writing skill: `.agnet/skills/编写技能/SKILL.md`
- Plan-writing skill: `.agnet/skills/编写计划/SKILL.md`
- Code-review-request skill: `.agnet/skills/请求代码审查/SKILL.md`

## Routing

- `@工作树` or `@worktree` routes to skill `Git工作树`.
- Trigger rule: if user message contains `@工作树` or `@worktree`, load and follow `.agnet/skills/Git工作树/SKILL.md`.
- `@pdf` routes to skill `PDF文档处理`.
- Trigger rule: if user message contains `@pdf`, load and follow `.agnet/skills/PDF文档处理/SKILL.md`.
- `@py` routes to skill `Python专家`.
- Trigger rule: if user message contains `@py`, load and follow `.agnet/skills/Python专家/SKILL.md`.
- `@头脑风暴` or `@brainstorm` routes to skill `头脑风暴`.
- Trigger rule: if user message contains `@头脑风暴` or `@brainstorm`, load and follow `.agnet/skills/头脑风暴/SKILL.md`.
- `@验证` or `@verify` routes to skill `完成前验证`.
- Trigger rule: if user message contains `@验证` or `@verify`, load and follow `.agnet/skills/完成前验证/SKILL.md`.
- `@并行代理` or `@parallel-agents` routes to skill `并行代理调度`.
- Trigger rule: if user message contains `@并行代理` or `@parallel-agents`, load and follow `.agnet/skills/并行代理调度/SKILL.md`.
- `@执行计划` or `@execute-plan` routes to skill `执行计划`.
- Trigger rule: if user message contains `@执行计划` or `@execute-plan`, load and follow `.agnet/skills/执行计划/SKILL.md`.
- `@TDD` or `@测试驱动` routes to skill `测试驱动开发`.
- Trigger rule: if user message contains `@TDD` or `@测试驱动`, load and follow `.agnet/skills/测试驱动开发/SKILL.md`.
- `@编写技能` or `@write-skill` routes to skill `编写技能`.
- Trigger rule: if user message contains `@编写技能` or `@write-skill`, load and follow `.agnet/skills/编写技能/SKILL.md`.
- `@编写计划` or `@write-plan` routes to skill `编写计划`.
- Trigger rule: if user message contains `@编写计划` or `@write-plan`, load and follow `.agnet/skills/编写计划/SKILL.md`.
- `@请求审查` or `@code-review` routes to skill `请求代码审查`.
- Trigger rule: if user message contains `@请求审查` or `@code-review`, load and follow `.agnet/skills/请求代码审查/SKILL.md`.
- Scope rule: project-level only; do not use system-level skill directories for this route.
