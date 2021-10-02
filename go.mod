module github.com/davidoram/bluetooth

go 1.16

require (
	github.com/paypal/gatt v0.0.1
	github.com/rs/zerolog v1.25.0
)

replace github.com/paypal/gatt v0.0.1 => github.com/davidoram/gatt v0.0.2-0.20210928071316-5776ec39d1bb
