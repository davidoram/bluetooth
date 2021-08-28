# bluetooth
Bluetooth client and server for simple communication

```
env GOOS=linux GOARCH=arm64 go build
scp ./bluetooth user@host
```


On the Raspberry Pi 4 running  Ubuntu 21.04 (GNU/Linux 5.11.0-1016-raspi aarch64):

```

# Run the app
sudo ./bluetooth
```

Using bluetooth low energy (BTLE), we have the following:

- peripheral. A server that can create services, characteristics, and descriptors, advertise, accept connections, and handle requests.
- central. A client that will scan, connect, discover services, and make requests

Uses [Gatt](https://learn.adafruit.com/introduction-to-bluetooth-low-energy/gatt) (Generic Attribute Profile) protocol.



