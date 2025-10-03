# 🛡️ Content Safety System

## Overview

The Content Safety System is a comprehensive web content filtering solution that prevents the AI system from accessing inappropriate adult websites, malicious content, and suspicious URLs. It provides multi-layered protection through domain filtering, keyword detection, and pattern matching.

## Features

### 🔒 **Multi-Layer Protection**

1. **Domain Blacklist**: Explicitly blocks known adult content and malicious sites
2. **Domain Whitelist**: Allows trusted educational, news, and technology sites
3. **Adult Content Detection**: Scans URLs and content for adult keywords
4. **Malicious Pattern Detection**: Identifies phishing, scam, and malware patterns
5. **Suspicious URL Detection**: Blocks IP addresses, URL shorteners, and suspicious domains

### 📊 **Comprehensive Logging**

- All blocked requests are logged with detailed reasons
- Tool metrics track blocked vs successful calls
- Recent calls show detailed error messages and parameters
- Metrics are stored in Redis for real-time monitoring

### ⚙️ **Configurable Safety Levels**

- **Strict**: Block everything not explicitly allowed (whitelist approach)
- **Moderate**: Block known bad, allow most others (current default)
- **Permissive**: Only block explicit threats

## Implementation

### Files Created/Modified

1. **`hdn/content_safety.go`**: Core safety system implementation
2. **`hdn/tools.go`**: Updated HTTP tool to use safety system
3. **`config/content_safety.yaml`**: Configuration file for safety rules
4. **`test_content_safety.sh`**: Test script for validation

### Key Components

#### ContentSafetyManager
- Manages blocked/allowed domains
- Handles keyword pattern matching
- Provides URL and content validation

#### SafeHTTPClient
- Wraps HTTP client with safety checks
- Enforces content size limits
- Validates URLs before requests

## Blocked Content Categories

### 🚫 **Adult Content Sites**
- pornhub.com, xvideos.com, xhamster.com
- redtube.com, youporn.com, tube8.com
- onlyfans.com, chaturbate.com

### 🚫 **Malicious Sites**
- malware.com, phishing.com, scam.com
- bitcoin-scam.com, fake-bank.com
- malicious-site.com

### 🚫 **Suspicious Patterns**
- IP addresses in URLs (192.168.1.1)
- URL shorteners (bit.ly, tinyurl)
- Free domains (.tk, .ml, .ga, .cf)

### 🚫 **Adult Content Keywords**
- Explicit sexual terms
- Adult service references
- Inappropriate content descriptors

## Allowed Content Categories

### ✅ **Educational Sites**
- wikipedia.org, mit.edu, stanford.edu
- coursera.org, edx.org, khanacademy.org

### ✅ **Technology Documentation**
- github.com, stackoverflow.com
- developer.mozilla.org, docs.python.org
- golang.org, nodejs.org, reactjs.org

### ✅ **News Sources**
- news.bbc.co.uk, reuters.com
- ap.org, npr.org, cnn.com

### ✅ **Standards Organizations**
- w3.org, ietf.org, rfc-editor.org
- iso.org, tools.ietf.org

## Usage

### Testing the System

```bash
# Run the comprehensive test suite
./test_content_safety.sh
```

### API Endpoints

The safety system integrates with the existing tool metrics API:

- `GET /api/v1/tools/metrics` - View tool usage statistics
- `GET /api/v1/tools/calls/recent` - View recent tool calls
- `POST /api/v1/tools/tool_http_get/invoke` - Make safe HTTP requests

### Example Usage

```bash
# Safe request (allowed)
curl -X POST http://localhost:8081/api/v1/tools/tool_http_get/invoke \
  -H "Content-Type: application/json" \
  -d '{"url": "https://github.com"}'

# Blocked request (adult content)
curl -X POST http://localhost:8081/api/v1/tools/tool_http_get/invoke \
  -H "Content-Type: application/json" \
  -d '{"url": "https://pornhub.com"}'
```

## Configuration

### Safety Rules

The system uses a configuration file (`config/content_safety.yaml`) to define:

- Blocked domains list
- Allowed domains list
- Adult content keywords
- Malicious patterns
- Content size limits
- Safety levels

### Customization

To add new blocked domains:

```yaml
blocked_domains:
  - "new-malicious-site.com"
  - "another-adult-site.com"
```

To add new allowed domains:

```yaml
allowed_domains:
  - "new-trusted-site.com"
  - "another-educational-site.org"
```

## Monitoring

### Tool Metrics

The system provides detailed metrics:

- Total tool calls
- Success vs failure counts
- Blocked request counts
- Average response times
- Last called timestamps

### Recent Calls

View recent tool calls with:
- Tool ID and parameters
- Success/failure status
- Error messages for blocked requests
- Timestamps

## Security Features

### Content Size Limits
- Maximum content length: 10MB
- Maximum URL length: 2KB
- Request timeout: 30 seconds

### Safe Headers
- User-Agent: Artificial Mind-Safe-Bot/1.0
- Accept: text/html,application/xhtml+xml,application/xml
- Accept-Language: en-US,en;q=0.9

### Error Handling
- Graceful failure for blocked requests
- Detailed error messages
- Proper HTTP status codes (403 Forbidden)

## Integration

The content safety system is fully integrated with:

1. **HDN Server**: Core API server handles tool invocation
2. **Tool Metrics**: Tracks and logs all tool usage
3. **Monitor UI**: Displays real-time metrics and status
4. **Redis**: Stores metrics and configuration data

## Testing Results

✅ **All Tests Passed**:
- Safe URLs are allowed
- Blocked URLs are properly blocked
- Suspicious URLs are blocked
- All blocked requests are logged
- Metrics are properly tracked

## Future Enhancements

1. **Machine Learning**: AI-powered content classification
2. **Real-time Updates**: Dynamic rule updates without restart
3. **Custom Policies**: User-defined safety policies
4. **Content Analysis**: Deep content inspection beyond keywords
5. **Whitelist Management**: Web interface for managing allowed domains

## Conclusion

The Content Safety System provides robust protection against inappropriate content while maintaining access to legitimate educational and technology resources. It's designed to be configurable, monitorable, and effective in preventing the AI system from accessing harmful or inappropriate content.

---

**Status**: ✅ **FULLY IMPLEMENTED AND TESTED**
**Last Updated**: September 27, 2025
**Version**: 1.0.0
