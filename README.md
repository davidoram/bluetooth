# bluetooth - not working

Bluetooth Low Energy client and server for simple communication. Not working

# Build instructions

Creating a GitHub release will trigger a action to build the following components:

- `btserver` : the server component
- `btclient` : the client component
- `fserver`  : a sample http file server (strictly for testing)


Tested on Raspberry Pi 3 & 4 running `Ubuntu 21.04 (GNU/Linux 5.11.0-1016-raspi aarch64)`

## On machine 1:

```
echo "hi" > hello.txt
# Start the http file server listening on `localhost:8100`
./fserver
```

```
# Start the bluetooth HPS server
# Will proxy incoming requests to an http server running locally
sudo ./btserver
```

## On machine 2:

```
# Call fserver over bluetooth
sudo ./btclient --url http://localhost:8100/hello.txt

```

# Bluetooth resources:

- [Gatt](https://learn.adafruit.com/introduction-to-bluetooth-low-energy/gatt) (Generic Attribute Profile) protocol.
- https://www.oreilly.com/library/view/getting-started-with/9781491900550/ch04.html
- [Bluetooth UUIDs](https://btprodspecificationrefs.blob.core.windows.net/assigned-values/16-bit%20UUID%20Numbers%20Document.pdf)
- Bluetooth [HTTP Proxy Service (HPS)](https://www.bluetooth.org/docman/handlers/downloaddoc.ashx?doc_id=308344) spec.


