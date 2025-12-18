# Relevance Filtering Improvements - Learning More Relevant Knowledge

## Overview

I've improved the Artificial Mind's learning system to focus on **relevant, useful knowledge** rather than learning everything equally. The system now uses **LLM-based intelligent filtering** to assess novelty and value, checks for existing knowledge, and filters facts and concepts based on relevance to user interests and goals, ensuring it learns what's actually useful and novel.

## Problem Identified

The system was learning everything equally:
- **Fact extraction** just wrapped entire input as a single fact
- **Concept discovery** didn't filter for relevance
- **No mechanism** to determine what knowledge is useful vs. just interesting
- **No prioritization** of actionable knowledge

## Improvements Implemented

### 1. **Improved Fact Extraction** ✅

**Before:** Just wrapped entire input as a single fact
```go
facts := []Fact{
    {
        Content: input,  // Just the whole input
        Confidence: 0.8,
    },
}
```

**After:** Uses LLM to extract meaningful, actionable facts
- Extracts specific facts from text (not just wrapping input)
- Identifies actionable/useful facts
- Scores relevance based on user interests
- Filters out vague or irrelevant facts

**Key Features:**
- `extractMeaningfulFacts()` - Uses LLM to extract actual facts
- Relevance scoring (0.0-1.0) based on user interests
- Actionability detection
- Filters facts with relevance < 0.4

### 2. **Relevance Scoring** ✅

**Added:**
- `calculateRelevance()` - Calculates relevance score for facts
- `filterByRelevance()` - Filters facts by relevance threshold (0.4)
- Relevance boost for:
  - Facts mentioning user interests
  - Actionable keywords ("can", "should", "how to", etc.)
- Relevance penalty for vague facts

**Scoring Factors:**
- User interest matches (+0.15 per match)
- Actionable keywords (+0.1)
- Vague keywords (-0.1)
- Base relevance: 0.5

### 3. **User Interest Tracking** ✅

**Added:**
- `getUserInterests()` - Retrieves user interests from Redis
- Falls back to recent goals if interests not set
- Uses interests to filter facts and concepts

**Sources:**
1. `user:interests` Redis key (primary)
2. Recent curiosity goals (fallback)
3. Default: "general knowledge, problem solving, task completion"

### 4. **Concept Discovery Relevance Filtering** ✅

**Improved:**
- Updated LLM prompt to focus on useful, relevant concepts
- Added relevance scoring to concept extraction
- Filters concepts with relevance < 0.4
- Prioritizes actionable, practical concepts

**Prompt Improvements:**
- Emphasizes "USEFUL" and "RELEVANT" concepts
- Focuses on actionable concepts
- Skips abstract/theoretical concepts without practical use
- Relates to user interests

### 5. **Knowledge Filtering** ✅

**Before:** Learned everything equally

**After:** 
- Filters facts with relevance < 0.4
- Filters concepts with relevance < 0.4
- Logs filtered items for monitoring
- Only stores relevant, useful knowledge

### 6. **LLM-Based Novelty Assessment** ✅ (NEW)

**Added:**
- `assessKnowledgeValue()` - Uses LLM to assess if knowledge is novel and worth learning
- `knowledgeAlreadyExists()` - Checks if knowledge already exists before storing
- `assessConceptValue()` - Assesses concept novelty and value
- `conceptAlreadyExists()` - Checks if concept already exists

**Key Features:**
- **Novelty Assessment**: LLM evaluates if knowledge is genuinely new or just obvious/common knowledge
- **Value Assessment**: LLM evaluates if knowledge is actionable and worth storing
- **Duplicate Prevention**: Checks knowledge base before storing to prevent duplicates
- **Intelligent Filtering**: Only stores knowledge that is novel, valuable, and doesn't already exist

**Assessment Criteria:**
- **Novelty**: Is this knowledge new/novel, or is it already obvious/known?
- **Value**: Will this help accomplish tasks or solve problems?
- **Existence**: Does this knowledge already exist in the knowledge base?

**Benefits:**
- Prevents storing obvious/common knowledge
- Avoids duplicate knowledge storage
- Focuses on actionable, useful knowledge
- Uses LLM intelligence to make filtering decisions

## Implementation Details

### Fact Extraction Flow

```
User Input
    ↓
extractMeaningfulFacts() [LLM-based extraction]
    ↓
Extract specific facts with relevance scores
    ↓
filterByRelevance() [Threshold: 0.4]
    ↓
assessKnowledgeValue() [LLM-based novelty/value assessment]
    ↓
knowledgeAlreadyExists() [Check for duplicates]
    ↓
Store only novel, valuable, non-duplicate facts
```

### Concept Discovery Flow

```
Text Analysis
    ↓
extractConceptsWithLLM() [With relevance focus]
    ↓
Extract concepts with relevance scores
    ↓
Filter concepts with relevance < 0.4
    ↓
conceptAlreadyExists() [Check for duplicates]
    ↓
assessConceptValue() [LLM-based novelty/value assessment]
    ↓
Store only novel, valuable, non-duplicate concepts
```

## Key Functions

### `extractMeaningfulFacts(input, domain, concepts)`
- Uses LLM to extract actual facts from text
- Scores relevance based on user interests
- Returns only actionable facts with relevance >= 0.3

### `filterByRelevance(facts, domain)`
- Filters facts by relevance threshold (0.4)
- Updates confidence scores based on relevance
- Logs filtered facts for monitoring

### `calculateRelevance(factContent, userInterests, domain)`
- Calculates relevance score (0.0-1.0)
- Considers user interests, actionable keywords
- Penalizes vague facts

### `getUserInterests()`
- Retrieves user interests from Redis
- Falls back to recent goals
- Returns default interests if none found

### `assessKnowledgeValue(knowledge, domain)` (NEW)
- Uses LLM to assess if knowledge is novel and worth learning
- Returns `(isNovel, isWorthLearning, error)`
- Evaluates novelty (is it new or obvious?) and value (is it useful?)
- Filters out obvious/common knowledge

### `knowledgeAlreadyExists(knowledge, domain)` (NEW)
- Checks if knowledge already exists in the knowledge base
- Queries HDN knowledge API for similar concepts
- Prevents duplicate storage

### `assessConceptValue(conceptKnowledge, domain)` (NEW)
- Similar to `assessKnowledgeValue` but for concepts
- Assesses if a concept is novel and worth learning

### `conceptAlreadyExists(conceptName, domain)` (NEW)
- Checks if a concept already exists before storing
- Prevents duplicate concept creation

## Expected Outcomes

After these improvements:

1. **More Relevant Facts:** System learns facts that are actually useful
2. **Better Concept Discovery:** Focuses on actionable concepts
3. **User-Focused Learning:** Learns what user cares about
4. **Reduced Noise:** Filters out irrelevant knowledge
5. **Actionable Knowledge:** Prioritizes knowledge that helps accomplish tasks
6. **Novel Knowledge Only:** Only stores genuinely new, non-obvious knowledge
7. **No Duplicates:** Prevents storing knowledge that already exists
8. **Intelligent Filtering:** Uses LLM to make smart decisions about what to learn

## Configuration

### Relevance Thresholds

- **Fact Relevance Threshold:** 0.4 (minimum relevance to store)
- **Concept Relevance Threshold:** 0.4 (minimum relevance to store)
- **Actionability Requirement:** Facts must be actionable to be stored

### User Interests

Set user interests in Redis:
```bash
redis-cli SET user:interests "your interests here"
```

Or let the system infer from recent goals automatically.

## Monitoring

The system logs:
- ✨ Extracted relevant facts/concepts
- 🛑 Filtered out low-relevance items
- 📊 Relevance filtering statistics

Example logs:
```
✨ Extracted relevant fact: How to parse JSON in Go (relevance: 0.85)
🛑 Filtered out low-relevance fact: Something about things (relevance: 0.25)
📊 Relevance filtering: 3 facts kept out of 5 (threshold: 0.4)
```

## Future Enhancements

1. **Dynamic Thresholds:** Adjust relevance thresholds based on domain
2. **Interest Learning:** Learn user interests from behavior
3. **Goal-Based Filtering:** Filter based on active goals
4. **Relevance Feedback:** User feedback to improve relevance scoring
5. **Domain-Specific Relevance:** Different relevance criteria per domain

## Conclusion

The Artificial Mind now learns **more relevant, useful knowledge** by:
- Extracting meaningful facts (not just wrapping input)
- Scoring relevance based on user interests
- Filtering out irrelevant knowledge
- Prioritizing actionable, useful information
- **Using LLM to assess novelty and value** (NEW)
- **Checking for existing knowledge before storing** (NEW)
- **Preventing duplicate knowledge storage** (NEW)
- **Filtering out obvious/common knowledge** (NEW)

This makes the system more focused and useful, learning what actually matters rather than everything equally. The system now uses **intelligent LLM-based filtering** to ensure it only learns novel, valuable knowledge that doesn't already exist.

