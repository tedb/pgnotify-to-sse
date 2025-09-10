# PostgreSQL Notification to SSE Bridge

A high-performance Go service that bridges PostgreSQL `NOTIFY` messages to web clients using Server-Sent Events (SSE). This enables real-time communication between your database and web applications with minimal latency and automatic reconnection handling.

## ✨ Features

- **Real-time Communication**: Instantly forward PostgreSQL notifications to web clients
- **Server-Sent Events**: Standards-based SSE implementation with automatic reconnection
- **UUID-based Topics**: Secure topic isolation using UUID validation
- **Graceful Shutdown**: Clean resource management and connection handling
- **Web Interface**: Built-in HTML interface for testing and monitoring
- **High Concurrency**: Efficient handling of multiple clients and topics
- **Automatic Cleanup**: Smart resource management when clients disconnect
- **Docker Support**: Easy deployment with containerization

## 🚀 Quick Start

### Prerequisites

- Go 1.24+ 
- PostgreSQL 12+
- Docker (optional, for local development)

### Installation

1. **Clone the repository**
   ```bash
   git clone <your-repo-url>
   cd pgnotify-to-sse
   ```

2. **Install dependencies**
   ```bash
   go mod download
   ```

3. **Start PostgreSQL with Docker**
   ```bash
   # Start PostgreSQL container
   docker run --name postgres-notify \
     -e POSTGRES_PASSWORD=postgres \
     -e POSTGRES_USER=postgres \
     -e POSTGRES_DB=postgres \
     -p 5432:5432 \
     -d postgres:15
   
   # Verify connection
   docker exec -it postgres-notify psql -U postgres -c "SELECT version();"
   ```

4. **Run the application**
   ```bash
   go run main.go
   ```

5. **Open your browser**
   Navigate to http://localhost:8080 to access the web interface.

## 🔧 Configuration

The application can be configured using environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | `postgres://postgres:postgres@localhost/postgres?sslmode=disable` | PostgreSQL connection string |
| `SERVER_ADDRESS` | `:8080` | HTTP server bind address |

### Example Configuration

```bash
export DATABASE_URL="postgres://user:pass@localhost:5432/mydb?sslmode=require"
export SERVER_ADDRESS=":3000"
go run main.go
```

## 📡 API Endpoints

### GET /
Web interface for testing and monitoring connections.

### GET /subscribe?topic={uuid}
Server-Sent Events endpoint for subscribing to notifications.

**Parameters:**
- `topic` (required): Valid UUID identifying the notification channel

**Response Headers:**
- `Content-Type: text/event-stream`
- `Cache-Control: no-cache`
- `Connection: keep-alive`
- `Access-Control-Allow-Origin: *`

**Example:**
```bash
curl -N "http://localhost:8080/subscribe?topic=550e8400-e29b-41d4-a716-446655440000"
```

## 💾 Database Usage

### Sending Notifications

Use PostgreSQL's `NOTIFY` command to send messages:

```sql
-- Send notification to a specific UUID topic
NOTIFY "550e8400-e29b-41d4-a716-446655440000", 'Your message payload here';

-- Example with a trigger function
CREATE OR REPLACE FUNCTION notify_order_update()
RETURNS TRIGGER AS $$
BEGIN
    -- Notify using the order ID as UUID
    PERFORM pg_notify(NEW.id::text, 'order_updated');
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER order_update_trigger
    AFTER UPDATE ON orders
    FOR EACH ROW
    EXECUTE FUNCTION notify_order_update();
```

### Testing Notifications

Connect to your PostgreSQL database and send test notifications:

```sql
-- Connect to PostgreSQL
psql -U postgres -h localhost

-- Send a test notification
NOTIFY "550e8400-e29b-41d4-a716-446655440000", 'Hello from PostgreSQL!';
```

## 🌐 Client Integration

### JavaScript/Browser

```javascript
const uuid = '550e8400-e29b-41d4-a716-446655440000';
const eventSource = new EventSource(`/subscribe?topic=${uuid}`);

eventSource.onmessage = function(event) {
    console.log('Received:', event.data);
    // Handle the notification
};

eventSource.onerror = function(event) {
    console.error('SSE error:', event);
    // Handle connection errors (automatic reconnection)
};

// Clean up when done
// eventSource.close();
```

### Node.js

```javascript
const EventSource = require('eventsource');

const uuid = '550e8400-e29b-41d4-a716-446655440000';
const eventSource = new EventSource(`http://localhost:8080/subscribe?topic=${uuid}`);

eventSource.onmessage = function(event) {
    console.log('Received:', event.data);
};
```

### cURL

```bash
# Subscribe to notifications
curl -N "http://localhost:8080/subscribe?topic=550e8400-e29b-41d4-a716-446655440000"
```

## 🐳 Docker Deployment

### Running with Docker Compose

```bash
# Start the complete stack
docker-compose up -d

# View logs
docker-compose logs -f pgnotify-sse

# Stop the stack
docker-compose down
```

## 🧪 Testing

Run the comprehensive test suite:

```bash
# Run all tests
go test -v

# Run tests with coverage
go test -v -cover

# Run specific test
go test -v -run TestMainNotificationFlow
```

### Manual Testing

1. **Start the application**
   ```bash
   go run main.go
   ```

2. **Open the web interface**
   Navigate to http://localhost:8080

3. **Generate a UUID and subscribe**
   ```bash
   # Generate UUID
   uuidgen
   # Example: 550e8400-e29b-41d4-a716-446655440000
   ```

4. **Send a notification from PostgreSQL**
   ```sql
   NOTIFY "550e8400-e29b-41d4-a716-446655440000", 'Test message';
   ```

5. **Verify message delivery**
   Check the web interface or SSE connection for the delivered message.

## 🏗️ Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   PostgreSQL    │    │  pgnotify-to-sse │    │   Web Client    │
│                 │    │                  │    │                 │
│  NOTIFY uuid,   ├────┤  Go Application  ├────┤  EventSource    │
│  'payload'      │    │                  │    │  /subscribe     │
│                 │    │  • Broker        │    │                 │
└─────────────────┘    │  • SSE Handler   │    └─────────────────┘
                       │  • UUID Topics   │
                       └──────────────────┘
```

### Components

- **Broker**: Manages subscriptions and message routing
- **Listener**: PostgreSQL notification listener with reconnection
- **Server**: HTTP server handling SSE connections
- **App**: Main application with dependency injection

## 🔒 Security Considerations

- **UUID Validation**: Only valid UUIDs are accepted as topics
- **Resource Limits**: Buffered channels prevent memory exhaustion  
- **Clean Shutdown**: Graceful handling of connections during shutdown
- **CORS Headers**: Configurable cross-origin resource sharing

## 📊 Performance

- **Concurrent Clients**: Efficiently handles thousands of concurrent SSE connections
- **Topic Isolation**: Each UUID topic is independently managed
- **Memory Efficient**: Automatic cleanup of unused resources
- **Low Latency**: Direct PostgreSQL to client message forwarding

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙋‍♂️ Support

- **Issues**: Report bugs and request features via GitHub Issues
- **Documentation**: Check the code comments for detailed API documentation
- **Examples**: See the `main_test.go` file for usage examples

---

**Made with ❤️ for real-time PostgreSQL applications**
