#!/bin/bash
# Ollama Devcontainer Startup Script
# This script starts the Ollama service and pre-pulls the all-minilm model

set -e

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Ollama Embedding Service Startup${NC}"
echo -e "${BLUE}========================================${NC}"

# Check if ollama is installed
if ! command -v ollama &> /dev/null; then
    echo -e "${YELLOW}Ollama not found, attempting to install...${NC}"
    curl -fsSL https://ollama.com/install.sh | sh || {
        echo -e "${RED}Failed to install Ollama${NC}"
        exit 0  # Don't fail the whole startup
    }
fi

# Create log directory
mkdir -p /var/log/ollama

# Start Ollama server in background if not already running
if pgrep -x "ollama" > /dev/null; then
    echo -e "${GREEN}Ollama is already running${NC}"
else
    echo -e "${YELLOW}Starting Ollama server...${NC}"
    nohup ollama serve > /var/log/ollama/server.log 2>&1 &
    OLLAMA_PID=$!
    echo $OLLAMA_PID > /tmp/ollama.pid

    # Wait for Ollama to be ready
    echo -e "${YELLOW}Waiting for Ollama to be ready...${NC}"
    MAX_RETRIES=30
    RETRY_COUNT=0
    READY=false

    while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
        if curl -s http://localhost:11434/api/tags >/dev/null 2>&1; then
            READY=true
            break
        fi
        sleep 1
        RETRY_COUNT=$((RETRY_COUNT + 1))
    done

    if [ "$READY" = false ]; then
        echo -e "${RED}Ollama failed to start within ${MAX_RETRIES} seconds${NC}"
        echo -e "${YELLOW}Continuing anyway - Ollama may start later${NC}"
    fi
fi

# Pull the all-minilm model for embedding tests
echo -e "${YELLOW}Checking for all-minilm model...${NC}"

# Check if model is already available
if curl -s http://localhost:11434/api/tags 2>/dev/null | grep -q "all-minilm"; then
    echo -e "${GREEN}all-minilm model is already available${NC}"
else
    echo -e "${YELLOW}Pulling all-minilm model (this may take a few minutes)...${NC}"
    if ollama pull all-minilm 2>&1; then
        echo -e "${GREEN}all-minilm model pulled successfully${NC}"
    else
        echo -e "${YELLOW}Failed to pull model - will retry on first use${NC}"
    fi
fi

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}Ollama Setup Complete${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "${BLUE}Available Services:${NC}"
echo -e "  ${GREEN}Ollama API:${NC}    http://localhost:11434"
echo -e "  ${GREEN}Model:${NC}         all-minilm (384-dimensional embeddings)"
echo ""
echo -e "${BLUE}Quick Test:${NC}"
echo -e "  ${YELLOW}curl http://localhost:11434/api/tags${NC}"
echo ""
echo -e "${BLUE}Logs:${NC}"
echo -e "  ${YELLOW}tail -f /var/log/ollama/server.log${NC}"
echo ""

# Create a helper script for stopping Ollama
cat > /tmp/stop-ollama.sh <<'STOP_SCRIPT'
#!/bin/bash
if [ -f /tmp/ollama.pid ]; then
    PID=$(cat /tmp/ollama.pid)
    if kill -0 $PID 2>/dev/null; then
        echo "Stopping Ollama (PID: $PID)..."
        kill $PID
        sleep 2
        if kill -0 $PID 2>/dev/null; then
            echo "Force killing Ollama..."
            kill -9 $PID
        fi
        rm -f /tmp/ollama.pid
        echo "Ollama stopped"
    else
        echo "Ollama is not running"
        rm -f /tmp/ollama.pid
    fi
else
    echo "No PID file found"
    pkill -x ollama 2>/dev/null && echo "Ollama stopped" || echo "Ollama is not running"
fi
STOP_SCRIPT

chmod +x /tmp/stop-ollama.sh
