# 🤝 Contributing to AGI Project

Thank you for your interest in contributing to the AGI project! This document provides guidelines and information for contributors.

## 🎯 How to Contribute

### 🐛 Bug Reports
- Use the GitHub issue tracker
- Include steps to reproduce the bug
- Provide system information (OS, Docker version, etc.)
- Include relevant logs

### ✨ Feature Requests
- Use the GitHub issue tracker with the "enhancement" label
- Describe the feature and its use case
- Consider the project's philosophy of "AI building AI"

### 🔧 Code Contributions
- Fork the repository
- Create a feature branch
- Make your changes
- Add tests if applicable
- Submit a pull request

## 🚀 Getting Started

### 1. Fork and Clone
```bash
git clone https://github.com/yourusername/agi-project.git
cd agi-project
```

### 2. Set Up Development Environment
```bash
# Copy environment template
cp env.example .env

# Edit configuration for your setup
nano .env

# Start development environment
./quick-start.sh --build
```

### 3. Make Changes
- Follow the existing code style
- Add comments for complex logic
- Update documentation as needed
- Test your changes thoroughly

### 4. Test Your Changes
```bash
# Run tests
make test

# Run integration tests
make test-integration

# Test specific components
make test-fsm
make test-hdn
make test-principles
```

### 5. Submit Pull Request
- Create a clear description of your changes
- Reference any related issues
- Ensure all tests pass
- Update documentation if needed

## 📋 Development Guidelines

### Code Style
- Follow Go conventions
- Use meaningful variable and function names
- Add comments for complex logic
- Keep functions focused and small

### Documentation
- Update relevant documentation
- Add examples for new features
- Keep README files up to date
- Document API changes

### Testing
- Write tests for new features
- Ensure existing tests still pass
- Test with different LLM providers
- Test error conditions

## 🏗️ Project Structure

```
agi/
├── docs/                    # Documentation
├── examples/               # Usage examples
├── fsm/                    # Finite State Machine
├── hdn/                    # Hierarchical Decision Network
├── principles/             # Ethical decision-making
├── monitor/                # Web UI
├── tools/                  # AI tools and capabilities
├── cmd/                    # Command-line applications
├── bin/                    # Built binaries
├── k3s/                    # Kubernetes manifests
└── scripts/                # Utility scripts
```

## 🧠 Areas for Contribution

### 🎯 High Priority
- **New AI Tools** - Extend the tool system with new capabilities
- **LLM Providers** - Add support for more LLM providers
- **UI Improvements** - Enhance the monitor interface
- **Documentation** - Improve guides and examples
- **Testing** - Add more comprehensive tests

### 🔧 Medium Priority
- **Performance** - Optimize system performance
- **Security** - Enhance security features
- **Monitoring** - Improve observability
- **Deployment** - Add more deployment options
- **Integration** - Better third-party integrations

### 🎨 Nice to Have
- **Mobile UI** - Mobile-friendly interface
- **Plugin System** - Extensible plugin architecture
- **Multi-language** - Support for more programming languages
- **Cloud Integration** - Better cloud deployment options

## 🐳 Docker Development

### Building Images
```bash
# Build all images
make docker-build

# Build specific image
docker build -t agi-fsm -f Dockerfile.fsm .
```

### Development with Docker
```bash
# Start development environment
docker-compose -f docker-compose.dev.yml up -d

# View logs
docker-compose logs -f

# Stop services
docker-compose down
```

## 🧪 Testing

### Unit Tests
```bash
# Run all unit tests
go test ./...

# Run tests for specific package
go test ./fsm/...

# Run tests with coverage
go test -cover ./...
```

### Integration Tests
```bash
# Run integration tests
make test-integration

# Test with specific LLM provider
LLM_PROVIDER=openai make test-integration
```

### Manual Testing
```bash
# Test basic functionality
curl http://localhost:8081/health

# Test thinking mode
curl -X POST http://localhost:8081/api/v1/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "Test", "show_thinking": true}'
```

## 📚 Documentation

### Writing Documentation
- Use clear, concise language
- Include code examples
- Add diagrams for complex concepts
- Keep documentation up to date

### Documentation Structure
- **README.md** - Project overview and quick start
- **docs/SETUP_GUIDE.md** - Detailed setup instructions
- **docs/CONFIGURATION_GUIDE.md** - Configuration options
- **docs/API_REFERENCE.md** - API documentation
- **docs/THINKING_MODE_README.md** - Thinking mode guide

## 🔍 Code Review Process

### For Contributors
1. Ensure your code follows the style guidelines
2. Add tests for new functionality
3. Update documentation as needed
4. Respond to review feedback promptly

### For Reviewers
1. Check code quality and style
2. Verify tests are adequate
3. Ensure documentation is updated
4. Test the changes if possible

## 🐛 Reporting Issues

### Bug Reports
Include:
- Clear description of the issue
- Steps to reproduce
- Expected vs actual behavior
- System information
- Relevant logs

### Security Issues
- Report security issues privately
- Use the GitHub security advisory feature
- Do not disclose publicly until fixed

## 📞 Getting Help

- **GitHub Issues** - For bugs and feature requests
- **GitHub Discussions** - For questions and ideas
- **Documentation** - Check the `/docs` folder first
- **Examples** - Look at the `/examples` folder

## 🎉 Recognition

Contributors will be:
- Listed in the project README
- Mentioned in release notes
- Invited to the contributors' hall of fame

## 📄 License

This project is licensed under the **MIT License with Attribution Requirement**.

### For Contributors:
- Your contributions will be licensed under the same license as the project
- You must preserve the original copyright notice: "Copyright (c) 2025 Steven Fisher"
- Any derivative works must include proper attribution to Steven Fisher
- See the [LICENSE](LICENSE) file for complete terms

### Attribution Requirements:
When contributing or using this software:
1. Include the original copyright notice
2. Display "Steven Fisher" in documentation and credits
3. Include the LICENSE file in your project
4. Preserve attribution in any derivative works

---

**Thank you for contributing to the AGI project! Together, we're building the future of AI. 🚀**
