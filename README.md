# PostgreSQL Notification to SSE Bridge

[![Tests](https://github.com/USERNAME/pgnotify-to-sse/workflows/Tests/badge.svg)](https://github.com/USERNAME/pgnotify-to-sse/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/USERNAME/pgnotify-to-sse)](https://goreportcard.com/report/github.com/USERNAME/pgnotify-to-sse)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A high-performance Go service that bridges PostgreSQL `NOTIFY` messages to web clients using Server-Sent Events (SSE). This enables real-time communication between your database and web applications with minimal latency and automatic reconnection handling.

## ✨ Features

- **Real-time Communication**: Instantly forward PostgreSQL notifications to web clients
- **Server-Sent Events**: Standards-based SSE implementation with automatic reconnection
- **Multi-Topic Support**: Subscribe to multiple UUID topics simultaneously with automatic multiplexing
- **Consistent JSON Format**: All responses use the same JSON structure for easy parsing
- **UUID-based Topics**: Secure topic isolation using UUID validation
- **Graceful Shutdown**: Clean resource management and connection handling
- **Web Interface**: Built-in HTML interface for testing single and multiple topics
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

### GET /subscribe?topics={uuid1,uuid2,...}
Server-Sent Events endpoint for subscribing to single or multiple notification topics.

**Parameters:**
- `topics` (required): Single UUID or comma-separated list of valid UUIDs

**Response Format:**
- All responses use consistent JSON format with topic identification

**Response Headers:**
- `Content-Type: text/event-stream`
- `Cache-Control: no-cache`
- `Connection: keep-alive`
- `Access-Control-Allow-Origin: *`

**Examples:**
```bash
# Single topic
curl -N "http://localhost:8080/subscribe?topics=550e8400-e29b-41d4-a716-446655440000"

# Multiple topics
curl -N "http://localhost:8080/subscribe?topics=550e8400-e29b-41d4-a716-446655440000,6ba7b810-9dad-11d1-80b4-00c04fd430c8"

# Spaces are automatically trimmed
curl -N "http://localhost:8080/subscribe?topics=uuid1, uuid2, uuid3"
```

**Response Format:**
```javascript
// All responses use consistent JSON format
data: {"topic":"550e8400-e29b-41d4-a716-446655440000","message":"yo"}
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

#### Single Topic Subscription
```javascript
const uuid = '550e8400-e29b-41d4-a716-446655440000';
const eventSource = new EventSource(`/subscribe?topics=${uuid}`);

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

#### Multiple Topics Subscription
```javascript
const topics = [
    '550e8400-e29b-41d4-a716-446655440000',
    '6ba7b810-9dad-11d1-80b4-00c04fd430c8',
    'f47ac10b-58cc-4372-a567-0e02b2c3d479'
];
const eventSource = new EventSource(`/subscribe?topics=${topics.join(',')}`);

eventSource.onmessage = function(event) {
    try {
        // Parse JSON format (used for all responses)
        const data = JSON.parse(event.data);
        console.log(`Message from ${data.topic}:`, data.message);
        
        // Handle different topics
        switch(data.topic) {
            case topics[0]:
                handleUserNotification(data.message);
                break;
            case topics[1]:
                handleOrderUpdate(data.message);
                break;
            case topics[2]:
                handleSystemAlert(data.message);
                break;
        }
    } catch (e) {
        console.error('Failed to parse message:', event.data);
    }
};

function handleUserNotification(message) {
    // Handle user-specific notifications
}

function handleOrderUpdate(message) {
    // Handle order updates
}

function handleSystemAlert(message) {
    // Handle system alerts
}
```

### Node.js

#### Single Topic
```javascript
const EventSource = require('eventsource');

const uuid = '550e8400-e29b-41d4-a716-446655440000';
const eventSource = new EventSource(`http://localhost:8080/subscribe?topics=${uuid}`);

eventSource.onmessage = function(event) {
    console.log('Received:', event.data);
};
```

#### Multiple Topics
```javascript
const EventSource = require('eventsource');

const topics = [
    '550e8400-e29b-41d4-a716-446655440000',
    '6ba7b810-9dad-11d1-80b4-00c04fd430c8'
];
const eventSource = new EventSource(
    `http://localhost:8080/subscribe?topics=${topics.join(',')}`
);

eventSource.onmessage = function(event) {
    try {
        const data = JSON.parse(event.data);
        console.log(`[${data.topic}] ${data.message}`);
    } catch (e) {
        console.error('Failed to parse message:', event.data);
    }
};
```

### cURL

```bash
# Single topic subscription
curl -N "http://localhost:8080/subscribe?topics=550e8400-e29b-41d4-a716-446655440000"

# Multiple topics subscription
curl -N "http://localhost:8080/subscribe?topics=550e8400-e29b-41d4-a716-446655440000,6ba7b810-9dad-11d1-80b4-00c04fd430c8"
```

## 🎯 Multi-Topic Use Cases

The multi-topic subscription feature is perfect for applications that need to monitor multiple related entities simultaneously:

### **Dashboard Applications**
```javascript
// Monitor user activity, system health, and order updates in one connection
const dashboardTopics = [
    'user-activity-feed',     // User login/logout events
    'system-health-alerts',   // Server status changes
    'order-processing-queue'  // New orders and updates
];
const eventSource = new EventSource(`/subscribe?topics=${dashboardTopics.join(',')}`);
```

### **Real-time Notifications**
```javascript
// Subscribe to all notification types for a user
const userNotifications = [
    `user-${userId}-messages`,     // Direct messages
    `user-${userId}-alerts`,       // System alerts
    `user-${userId}-updates`       // Profile/settings updates
];
const eventSource = new EventSource(`/subscribe?topics=${userNotifications.join(',')}`);
```

### **Multi-tenant Applications**
```javascript
// Monitor multiple tenant databases
const tenantTopics = [
    'tenant-a-events',
    'tenant-b-events', 
    'tenant-c-events'
];
const eventSource = new EventSource(`/subscribe?topics=${tenantTopics.join(',')}`);
```

### **Microservices Communication**
```javascript
// Listen to events from multiple services
const serviceTopics = [
    'payment-service-events',
    'inventory-service-events',
    'notification-service-events'
];
const eventSource = new EventSource(`/subscribe?topics=${serviceTopics.join(',')}`);
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
