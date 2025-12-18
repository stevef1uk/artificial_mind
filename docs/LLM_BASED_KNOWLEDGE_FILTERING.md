# LLM-Based Knowledge Filtering

## Overview

The Artificial Mind now uses **LLM-based intelligent filtering** to assess whether knowledge is worth learning and storing. This ensures the system only learns novel, valuable knowledge that doesn't already exist, preventing storage of obvious/common knowledge and duplicates.

## Problem Solved

Previously, the system would:
- Store all extracted facts and concepts regardless of novelty
- Learn obvious/common knowledge that everyone already knows
- Store duplicate knowledge that already exists
- Waste storage on knowledge that isn't actionable or useful

## Solution: LLM-Based Assessment

The system now uses LLM intelligence to:
1. **Assess Novelty**: Is this knowledge genuinely new or just obvious/common knowledge?
2. **Assess Value**: Is this knowledge actionable and worth storing?
3. **Check Existence**: Does this knowledge already exist in the knowledge base?

## Implementation

### For Facts (`knowledge_integration.go`)

#### `assessKnowledgeValue(knowledge, domain)`
Uses LLM to assess if a fact is novel and worth learning.

**Returns:**
- `isNovel` (bool): Is this knowledge genuinely new or obvious?
- `isWorthLearning` (bool): Is this knowledge actionable and useful?
- `error`: Any errors during assessment

**Assessment Criteria:**
- **Novelty**: Is this knowledge new/novel, or is it already obvious/known?
- **Value**: Will this help accomplish tasks or solve problems?
- **Existence**: Does this knowledge already exist in the knowledge base?

#### `knowledgeAlreadyExists(knowledge, domain)`
Checks if knowledge already exists in the knowledge base before storing.

**Process:**
1. Extracts key terms from knowledge
2. Queries HDN knowledge API for similar concepts
3. Checks if any existing concept matches the new knowledge
4. Returns `true` if duplicate found, `false` otherwise

### For Concepts (`knowledge_growth.go`)

#### `assessConceptValue(conceptKnowledge, domain)`
Similar to `assessKnowledgeValue` but specifically for concepts.

**Process:**
1. Gets existing knowledge context from domain
2. Uses LLM to assess novelty and value
3. Returns assessment results

#### `conceptAlreadyExists(conceptName, domain)`
Checks if a concept already exists before storing.

**Process:**
1. Queries HDN knowledge API for concepts matching the name
2. Performs case-insensitive comparison
3. Returns `true` if concept exists, `false` otherwise

## Integration Points

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
assessKnowledgeValue() [LLM-based novelty/value assessment] ← NEW
    ↓
knowledgeAlreadyExists() [Check for duplicates] ← NEW
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
conceptAlreadyExists() [Check for duplicates] ← NEW
    ↓
assessConceptValue() [LLM-based novelty/value assessment] ← NEW
    ↓
Store only novel, valuable, non-duplicate concepts
```

## LLM Assessment Prompt

The system uses a carefully crafted prompt to assess knowledge:

```
Assess whether the following knowledge is worth learning and storing.

Knowledge to assess: [knowledge]
Domain: [domain]
Existing knowledge in domain: [existing concepts]

Evaluate:
1. NOVELTY: Is this knowledge new/novel, or is it already obvious/known?
   - Consider if this is common knowledge that everyone knows
   - Consider if this is already covered by existing knowledge
   - Consider if this is just restating something obvious

2. VALUE: Is this knowledge worth storing?
   - Will this help accomplish tasks or solve problems?
   - Is this actionable and useful?
   - Is this specific enough to be valuable?
   - Will this help with future learning or decision-making?

Return JSON:
{
  "is_novel": true/false,
  "is_worth_learning": true/false,
  "reasoning": "Brief explanation of why",
  "novelty_score": 0.0-1.0,
  "value_score": 0.0-1.0
}

Be strict: Only mark as novel and worth learning if:
- The knowledge is genuinely new or adds meaningful detail
- The knowledge is actionable and useful
- The knowledge is not obvious/common knowledge
- The knowledge is not already covered by existing knowledge
```

## Benefits

1. **Prevents Obvious Knowledge**: Filters out common knowledge that everyone knows
2. **Avoids Duplicates**: Checks for existing knowledge before storing
3. **Focuses on Value**: Only stores actionable, useful knowledge
4. **Intelligent Decisions**: Uses LLM intelligence to make filtering decisions
5. **Reduced Storage**: Saves storage space by not storing redundant knowledge
6. **Better Quality**: Higher quality knowledge base with only novel, valuable knowledge

## Example Scenarios

### Scenario 1: Obvious Knowledge Filtered Out

**Input:** "Water is wet"

**Assessment:**
- `is_novel`: false (obvious/common knowledge)
- `is_worth_learning`: false
- **Result:** Not stored

### Scenario 2: Novel Knowledge Stored

**Input:** "Matrix multiplication requires compatible dimensions: A.cols == B.rows"

**Assessment:**
- `is_novel`: true (specific, actionable knowledge)
- `is_worth_learning`: true (useful for code generation)
- **Result:** Stored

### Scenario 3: Duplicate Knowledge Prevented

**Input:** "Matrix Multiplication" (concept already exists)

**Check:**
- `conceptAlreadyExists()`: true
- **Result:** Not stored (duplicate)

### Scenario 4: Valuable Knowledge Stored

**Input:** "Error handling in Go requires checking returned error values before using results"

**Assessment:**
- `is_novel`: true (specific, actionable)
- `is_worth_learning`: true (useful for code generation)
- **Result:** Stored

## Configuration

The assessment is automatic and uses the HDN interpret endpoint. No special configuration is required.

## Monitoring

The system logs assessment results:

```
🧠 Knowledge assessment: novel=true, worth_learning=true, reasoning="Specific actionable knowledge"
⏭️ Skipping fact (not novel/obvious): Water is wet
⏭️ Skipping fact (already known): Matrix multiplication
✅ Stored novel fact: Matrix multiplication requires compatible dimensions
```

## Future Enhancements

1. **Adaptive Thresholds**: Adjust novelty/value thresholds based on domain
2. **Learning from Feedback**: Improve assessment based on user feedback
3. **Semantic Similarity**: Use vector embeddings for better duplicate detection
4. **Confidence Scoring**: Track assessment confidence over time
5. **Domain-Specific Assessment**: Different criteria per domain

## Conclusion

The LLM-based knowledge filtering system ensures the Artificial Mind only learns **novel, valuable knowledge** that doesn't already exist. This makes the system more efficient, focused, and useful by preventing storage of obvious/common knowledge and duplicates.

