# Auto Tool Creation Agent

## Overview

The Auto Tool Creation Agent monitors successful code executions and automatically creates reusable tools from code that is general enough to be reused. This enables the system to learn and expand its capabilities over time.

## How It Works

### 1. Monitoring Successful Executions

When code execution succeeds, the `recordSuccessfulExecution` function is called, which:
- Records learning metrics (success rate, retry count, etc.)
- Triggers the tool creation analysis via `considerToolCreationFromExecution`

### 2. LLM-Based Code Analysis

The `isCodeGeneralEnoughForTool` function uses an LLM to evaluate whether the code aligns with the system's main objectives and is suitable for tool creation.

**LLM Evaluation Process:**
1. Builds a comprehensive prompt that includes:
   - System objectives (autonomous execution, knowledge management, goal achievement, etc.)
   - Evaluation criteria (generality, reusability, parameterization, meaningful capability)
   - The code, language, and task description to evaluate

2. Calls the LLM with low priority (background task) to evaluate the code

3. Parses the LLM response (JSON format) to get:
   - `should_create_tool`: boolean recommendation
   - `reason`: brief explanation of the decision

**Evaluation Criteria (provided to LLM):**
- Is general/reusable enough to be useful in multiple contexts (not task-specific)
- Aligns with system objectives (autonomous execution, knowledge management, goal achievement)
- Would be useful for future autonomous task execution
- Has clear inputs and outputs (can be parameterized)
- Represents a meaningful capability (not trivial one-liners)

**Basic Pre-filtering:**
- Minimum 100 characters of code (skips trivial code)
- LLM client must be available (falls back gracefully if not)

### 3. Tool ID Generation

The `generateToolIDFromCode` function creates a stable tool ID:
- First tries to use the task name (if it's generic)
- Otherwise generates from code characteristics (language, hash, keyword score)

### 4. Duplicate Prevention

Before creating a tool, `toolExists` checks if a tool with the same ID already exists by querying the HDN API.

### 5. Tool Definition Creation

The `createToolFromCode` function builds a tool definition:
- Extracts input schema from execution context
- Creates output schema (output, success)
- Sets appropriate permissions and safety level
- Includes the code in the `exec` field

### 6. Tool Registration

The `registerToolViaAPI` function registers the tool via the HDN API:
- POSTs to `/api/v1/tools`
- Handles errors gracefully

## Configuration

The agent uses the `hdnBaseURL` configured in the `IntelligentExecutor`:
- Default: `http://localhost:8080`
- Can be set via environment variable or configuration

## Example Flow

1. User requests: "Parse JSON data from a URL"
2. LLM generates Python code to fetch and parse JSON
3. Code executes successfully
4. Agent analyzes code:
   - ✅ Has structure (function definition)
   - ✅ Contains utility signals (http, json, parse)
   - ✅ Not task-specific
5. Agent creates tool: `tool_parse_json_util`
6. Tool is registered and available for future use

## Logging

The agent logs its activities:
- `🔍 [TOOL-CREATOR]` - Analysis decisions
- `🔧 [TOOL-CREATOR]` - Tool creation attempts
- `✅ [TOOL-CREATOR]` - Successful tool creation
- `⚠️ [TOOL-CREATOR]` - Warnings/errors

## Benefits

1. **Intelligent Evaluation**: LLM understands context and system objectives better than rule-based heuristics
2. **Self-Improvement**: System learns from successful executions
3. **Code Reuse**: General utilities become available as tools
4. **Reduced Redundancy**: Avoids regenerating similar code
5. **Tool Discovery**: Automatically discovers useful patterns aligned with system goals
6. **Context-Aware**: LLM can evaluate code in the context of the system's autonomous objectives

## Limitations

1. **Code Quality**: Only creates tools from successful executions
2. **LLM Dependency**: Requires LLM client to be available (falls back gracefully if not)
3. **Parameter Inference**: Input schema is inferred from context, may not be perfect
4. **No Code Refactoring**: Uses code as-is, doesn't refactor for reusability
5. **LLM Response Parsing**: Relies on LLM returning valid JSON (has fallback parsing)

## Future Enhancements

- Use LLM to refactor code for better reusability
- Analyze multiple successful executions to create more robust tools
- Extract parameters more intelligently from code analysis
- Support for multiple language tools
- Tool versioning and updates

