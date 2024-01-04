package api

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

func NewApiClient(address string, key string, cert string, cacert string) (DesktopAutoscalerServiceClient, error) {
	var dialOpt grpc.DialOption

	certPool := x509.NewCertPool()

	if len(cert) == 0 {
		dialOpt = grpc.WithTransportCredentials(insecure.NewCredentials())
	} else if certFile, err := os.ReadFile(cert); err != nil {
		return nil, fmt.Errorf("could not open Cert configuration file %q: %v", cert, err)
	} else if keyFile, err := os.ReadFile(key); err != nil {
		return nil, fmt.Errorf("could not open Key configuration file %q: %v", key, err)
	} else if cacertFile, err := os.ReadFile(cacert); err != nil {
		return nil, fmt.Errorf("could not open Cacert configuration file %q: %v", cacert, err)
	} else if cert, err := tls.X509KeyPair(certFile, keyFile); err != nil {
		return nil, fmt.Errorf("failed to parse cert key pair: %v", err)
	} else if !certPool.AppendCertsFromPEM(cacertFile) {
		return nil, fmt.Errorf("failed to parse ca: %v", err)
	} else {
		transportCreds := credentials.NewTLS(&tls.Config{
			ServerName:   "localhost",
			Certificates: []tls.Certificate{cert},
			RootCAs:      certPool,
		})

		dialOpt = grpc.WithTransportCredentials(transportCreds)
	}

	if conn, err := grpc.Dial(address, dialOpt); err != nil {
		return nil, fmt.Errorf("failed to dial server: %v", err)
	} else {
		client := NewDesktopAutoscalerServiceClient(conn)

		if _, err = client.VMWareListVirtualMachines(context.Background(), &VirtualMachinesRequest{}); err != nil {
			return client, err
		}

		return client, err
	}
}
