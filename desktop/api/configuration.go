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

type Configuration struct {
	Address string `json:"address"` // external cluster autoscaler provider address of the form "host:port", "host%zone:port", "[host]:port" or "[host%zone]:port"
	Key     string `json:"key"`     // path to file containing the tls key
	Cert    string `json:"cert"`    // path to file containing the tls certificate
	Cacert  string `json:"cacert"`  // path to file containing the CA certificate
	client  VMWareDesktopAutoscalerServiceClient
}

func (c *Configuration) GetClient() (VMWareDesktopAutoscalerServiceClient, error) {

	if c.client == nil {
		var dialOpt grpc.DialOption

		certPool := x509.NewCertPool()

		if len(c.Cert) == 0 {
			dialOpt = grpc.WithTransportCredentials(insecure.NewCredentials())
		} else if certFile, err := os.ReadFile(c.Cert); err != nil {
			return nil, fmt.Errorf("could not open Cert configuration file %q: %v", c.Cert, err)
		} else if keyFile, err := os.ReadFile(c.Key); err != nil {
			return nil, fmt.Errorf("could not open Key configuration file %q: %v", c.Key, err)
		} else if cacertFile, err := os.ReadFile(c.Cacert); err != nil {
			return nil, fmt.Errorf("could not open Cacert configuration file %q: %v", c.Cacert, err)
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

		if conn, err := grpc.Dial(c.Address, dialOpt); err != nil {
			return nil, fmt.Errorf("failed to dial server: %v", err)
		} else {
			c.client = NewVMWareDesktopAutoscalerServiceClient(conn)

			if _, err = c.client.ListVirtualMachines(context.Background(), &VirtualMachinesRequest{}); err != nil {
				return c.client, err
			}
		}
	}

	return c.client, nil
}
