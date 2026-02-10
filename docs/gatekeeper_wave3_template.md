# Gatekeeper 清单（Lane D / Wave3）

## A. 统一门禁清单

1. 规范一致性
   - [ ] 新增模块命名、目录结构符合约定
   - [ ] 插件目录遵循 `plugins/<name>/plugin.py`

2. 功能完整性
   - [ ] `AgentSpec` 支持插件声明
   - [ ] loader 支持按声明名加载与校验
   - [ ] 至少 2 个示例插件可被发现/加载

3. 安全约束
   - [ ] 不开放任意 shell 插件能力
   - [ ] 插件接口仅白名单契约（`PLUGIN_NAME`/`PLUGIN_TOOLS`）

4. 测试门禁
   - [ ] 新增/修改模块有对应测试
   - [ ] `tests/test_plugin_loader.py` 通过
   - [ ] `tests/test_agent_factory.py` 通过
   - [ ] 相关回归测试通过（动态配置/启动参数）

5. 交付信息
   - [ ] 改动文件列表完整
   - [ ] 风险点与回滚点明确

## B. 执行结果模板（每轮复用）

```text
[Gatekeeper][Lane D][Wave3]
日期: YYYY-MM-DD
范围: 插件化骨架 + loader + AgentSpec 插件声明

检查结果:
- 规范一致性: PASS/FAIL（备注）
- 功能完整性: PASS/FAIL（备注）
- 安全约束: PASS/FAIL（备注）
- 测试门禁: PASS/FAIL（备注）

测试命令:
- <command 1>
- <command 2>

风险点:
- <risk 1>

回滚点:
- <rollback 1>

结论: GO / NO-GO
```
