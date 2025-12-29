# How to See Intelligence in Action

Your system has **6 failure patterns**, **2 strategies**, and **6 prevention hints** - the intelligence is working!

## Quick Verification

The intelligence system is **learning** (data exists). To see it **being used**, follow these steps:

## Method 1: Watch Logs While Generating Code

```bash
# Terminal 1: Watch for intelligence messages
HDN_POD=$(kubectl get pods -n agi | grep hdn | awk '{print $1}')
kubectl logs -n agi -f $HDN_POD | grep -i "intelligence\|learned\|prevention"

# Terminal 2: Trigger code generation
# Use your Monitor UI or API to generate code
# Look for messages like:
#   🧠 [INTELLIGENCE] Added X prevention hints from learned experience
#   🧠 [INTELLIGENCE] Retrieved learned prevention hint
```

## Method 2: Check Code Generation Prompts

The intelligence adds prevention hints to code generation prompts. To see this:

1. Generate code for a task that matches a learned pattern (e.g., Go code with imports)
2. Check HDN logs for the code generation prompt
3. Look for a section like:

```
🧠 LEARNED FROM EXPERIENCE - Common errors to avoid:
- Remove unused imports - they cause compilation errors
- Check for missing imports or typos in function/variable names
```

## Method 3: Compare Retry Counts

```bash
# First execution (may have retries)
# Generate code that matches a learned failure pattern

# Second similar execution (should have fewer retries)
# Generate similar code again

# The second should have fewer retries because:
# - Prevention hints are added to the prompt
# - The system avoids known failure patterns
```

## What Your Learning Data Shows

From your verification:
- **failure_pattern:validation:other:python** - seen **40 times**!
  - This pattern will definitely trigger prevention hints
- **failure_pattern:compilation:undefined_symbol:go** - seen 2 times
- **codegen_strategy:general:go** - 10% success rate
- **codegen_strategy:file_operations:go** - 10% success rate

## Expected Behavior

When you generate code that matches these patterns:

1. **Before code generation**: System retrieves prevention hints
2. **During prompt building**: Hints are added to the prompt
3. **In logs**: You'll see `🧠 [INTELLIGENCE] Added X prevention hints`
4. **Result**: Code should avoid the learned failure patterns

## Test It Now

```bash
# Run the usage test
./test_intelligence_usage.sh

# Or manually:
# 1. Watch logs
kubectl logs -n agi -f <hdn-pod> | grep -i intelligence

# 2. Generate Python code (matches the pattern seen 40 times!)
# Use Monitor UI or API to generate Python code

# 3. Watch for intelligence messages in the logs
```

## Success Indicators

You'll know intelligence is being used when you see:

✅ Logs show: `🧠 [INTELLIGENCE] Added X prevention hints`  
✅ Logs show: `🧠 [INTELLIGENCE] Retrieved learned prevention hint`  
✅ Code generation prompts include: `🧠 LEARNED FROM EXPERIENCE`  
✅ Similar tasks have fewer retries over time  
✅ Code avoids known failure patterns

Your system is **learning** (proven by the data). Now verify it's **using** that learning!

