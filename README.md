# bluetooth

Bluetooth Low Energy client and server for simple communication

To compile and install `peripheral` & `central` components:
```
# Install peripheral & static_fs to our server machine
env GOOS=linux GOARCH=arm64 go build -o btserver peripheral/main.go && scp ./btserver user@host:~
env GOOS=linux GOARCH=arm64 go build -o fserver fileserver/main.go && scp ./fserver user@host:~

# Install central to our client machine
env GOOS=linux GOARCH=arm64 go build -o btclient central/main.go  && scp ./btclient user@host:~
```


Tested on Raspberry Pi 4 running `Ubuntu 21.04 (GNU/Linux 5.11.0-1016-raspi aarch64)`


Using bluetooth low energy (BTLE):

- peripheral. A server that can create services, characteristics, and descriptors, advertise, accept connections, and handle requests.
- central. A client that will scan, connect, discover services, and make requests

Uses [Gatt](https://learn.adafruit.com/introduction-to-bluetooth-low-energy/gatt) (Generic Attribute Profile) protocol.
https://www.oreilly.com/library/view/getting-started-with/9781491900550/ch04.html


