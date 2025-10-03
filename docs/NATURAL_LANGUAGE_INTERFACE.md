# Natural Language Interface for Artificial Mind System

## 🎯 Overview

The Artificial Mind system now supports natural language input through the Monitor UI, making it accessible to users without technical knowledge of APIs or structured commands.

## 🚀 Features

### **Natural Language Input**
- **Simple Text Input**: Describe what you want to do in plain English
- **Multi-step Detection**: Automatically breaks down complex requests
- **Context Awareness**: Maintains conversation context and session management
- **Real-time Feedback**: Shows interpretation and execution progress

### **Two Operation Modes**
1. **Interpret Only**: Parse natural language into structured tasks
2. **Interpret & Execute**: Parse and immediately execute tasks

## 🖥️ User Interface

### **Monitor UI Integration**
- **Location**: http://localhost:8082
- **Natural Language Card**: Prominent input area at the top of the dashboard
- **Real-time Results**: Live display of interpretation and execution results
- **File Access**: Direct access to generated files and outputs

### **Input Examples**
```
"Find the first 10 prime numbers"
"Calculate the first 20 primes and show me a graph of distribution"
"Create a bar chart of sales data"
"Analyze the data and generate a report"
"Generate a PDF report with charts and analysis"
```

## 🔧 Technical Implementation

### **Architecture**
```
User Input → Monitor UI → HDN Interpreter → LLM Processing → Task Execution → Results Display
                               │
                               └──► Publish Canonical Events to NATS (agi.events.input)
Downstream: Planner / Belief Store / Auditor subscribe to event subjects
```

### **Components**
1. **Interpreter Package** (`hdn/interpreter/`)
   - Natural language parsing
   - Multi-step request detection
   - LLM integration

2. **Monitor UI Integration**
   - Natural language input interface
   - Real-time result display
   - File access and visualization

3. **API Endpoints**
   - `POST /api/interpret` - Parse natural language
   - `POST /api/interpret/execute` - Parse and execute

4. **Capability + Inputs Mapping**
   - The interpreter maps user text to a concrete capability (`task_name`/`id`) plus structured `inputs`.
   - If explicit inputs are missing, inputs fall back to `capability.context.inputs` defaults supplied by HDN.
   - The selected `project_id` is always propagated in the request context and header for scoping.

## 🎮 Usage

### **Basic Usage**
1. Open the Monitor UI: http://localhost:8082
2. Find the "🗣️ Natural Language Input" card
3. Enter your request in plain English
4. Click "Interpret" to see parsed tasks, or "Execute" to run immediately

### **Advanced Features**
- **Keyboard Shortcut**: Ctrl+Enter to execute
- **Session Management**: Automatic session tracking
- **Error Handling**: Clear error messages and recovery suggestions
- **File Access**: Direct links to generated files
- **Project Scoping**: Choose a project in the UI; interpreter and executor include it in all calls.
- **Cache-aware UX**: UI surfaces when results are served from cache vs cold-start regeneration.

## 📊 Example Workflows

### **Data Analysis Workflow**
```
Input: "Analyze the sales data and create a comprehensive report"

Interpretation:
1. LoadSalesData - Load and validate sales data
2. AnalyzeData - Perform statistical analysis
3. CreateVisualizations - Generate charts and graphs
4. GenerateReport - Create PDF report with findings

Execution: All tasks run automatically with file generation
```

### **Mathematical Computation Workflow**
```
Input: "Find the first 20 primes and show me a graph of distribution"

Interpretation:
1. CalculatePrimes - Calculate first 20 prime numbers
2. CreateDistributionGraph - Generate distribution visualization

Execution: Generates prime numbers and creates visualization files
```

## 🔄 Integration with Existing System

### **Seamless Integration**
- **No API Changes**: Existing APIs remain unchanged
- **Backward Compatibility**: All existing functionality preserved
- **Enhanced UX**: Natural language layer on top of existing system

### **Workflow Integration**
- **Hierarchical Planning**: Natural language requests use existing planner
- **Self-Model Learning**: Requests are tracked and learned from
- **File Generation**: Generated files accessible through Monitor UI
- **Real-time Monitoring**: Execution progress visible in dashboard (via NATS events)

## 🧪 Testing

### **Test Script**
```bash
./test_ui_integration.sh
```

### **Manual Testing**
1. Start all services:
   ```bash
   ./start_servers.sh
   ```

2. Open Monitor UI: http://localhost:8082

3. Try example inputs:
   - "Find the first 10 prime numbers"
   - "Create a simple bar chart"
   - "Generate a PDF report"

## 🎯 Benefits

### **For Users**
- **No Technical Knowledge Required**: Use plain English
- **Immediate Results**: See interpretation and execution in real-time
- **File Access**: Direct access to generated outputs
- **Error Recovery**: Clear error messages and suggestions

### **For Developers**
- **Extensible**: Easy to add new natural language patterns
- **Maintainable**: Clean separation of concerns
- **Testable**: Comprehensive test coverage
- **Scalable**: Built on existing robust architecture

## 🔮 Future Enhancements

### **Planned Features**
- **Conversation Memory**: Remember previous requests in session
- **Voice Input**: Speech-to-text integration
- **Advanced Visualizations**: Interactive charts and graphs
- **Template Library**: Pre-built request templates
- **Multi-language Support**: Support for multiple languages

### **Advanced Capabilities**
- **Context Awareness**: Remember user preferences and history
- **Smart Suggestions**: Suggest related tasks and improvements
- **Collaborative Features**: Share and discuss results
- **Integration APIs**: Connect with external tools and services

---

## 🚀 Quick Start

1. **Start Services**:
   ```bash
   ./start_servers.sh
   ```

2. **Open Monitor UI**: http://localhost:8082

3. **Try Natural Language**:
   - Enter: "Find the first 20 primes and show me a graph of distribution"
   - Click "Execute"
   - Watch the magic happen! ✨

The system is now ready for natural language interaction! 🎉

