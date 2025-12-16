# Network config protocol

Centauri can receive its route configuration over a TCP connection, allowing for dynamic
configuration updates. Set `CONFIG_SOURCE=network` and `CONFIG_NETWORK_ADDRESS=host:port`
to enable.

## Wire protocol

Each configuration is transmitted as:

1. **Magic bytes** (8 bytes): `CENTAURI`
2. **Version** (4 bytes): `0x00 0x00 0x00 0x01`
3. **Payload length** (4 bytes): big-endian uint32
4. **Payload** (variable): route configuration in standard [config format](routes.md)

## Behaviour

- Centauri connects to the configured address on startup
- Initial configuration must be received within 10 seconds or Centauri will reconnect and retry once
- After initial config is received, subsequent updates may take any amount of time
- Centauri applies configuration updates immediately as they are received
- If the connection is lost, Centauri will reconnect once and retry reading config
- If reconnection fails or the second read fails, Centauri exits with an error
- Invalid magic bytes or unsupported version cause disconnection and reconnection attempt
