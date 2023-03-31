package multicast

import (
	"github.com/go-logr/logr"
	"golang.org/x/net/ipv4"
	"k8s.io/klog/v2"
	"net"
)

const (
	maxDatagramSize = 8192
)

// Listen binds to the UDP address and port given and writes packets received
// from that address to a buffer which is passed to a hander
func Listen(address, ifaceName string, adr string, handler func(*net.UDPAddr, int, []byte), log logr.Logger) {
	// Parse the string address
	addr, err := net.ResolveUDPAddr("udp4", address)
	if err != nil {
		log.Error(err, "error resolving address")
	}

	/*
		iface, err := net.InterfaceByName("enp98s0f0")
		if err != nil {
			log.Error(err, "error resolving interface")
		}

		// Open up a connection
		conn, err := net.ListenMulticastUDP("udp4", iface, addr)
		if err != nil {
			log.Error(err, "error listening to multicast address")
		}

		conn.SetReadBuffer(maxDatagramSize)

		// Loop forever reading from the socket
		for {
			buffer := make([]byte, maxDatagramSize)
			numBytes, src, err := conn.ReadFromUDP(buffer)
			if err != nil {
				log.Error(err, "error reading from UDP")
			}

			handler(src, numBytes, buffer)
		}

	*/

	proto := "udp"

	// open socket (connection)
	conn, err := net.ListenPacket(proto, address)
	if err != nil {
		log.Error(err, "error listening")
	}

	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		log.Error(err, "error resolving interface")
	}

	// join multicast address
	a := net.ParseIP(adr)
	pc := ipv4.NewPacketConn(conn)
	if err = pc.JoinGroup(iface, &net.UDPAddr{IP: a}); err != nil {
		conn.Close()
		log.Error(err, "error joining")
	}

	go udpReader(pc, iface.Name, addr.String(), log)

}

func udpReader(c *ipv4.PacketConn, ifname, ifaddr string, log logr.Logger) {

	log.Info("udpReader: reading from %s on %s", ifaddr, ifname)

	defer c.Close()

	buf := make([]byte, 10000)

	for {
		n, _, _, err := c.ReadFrom(buf)
		if err != nil {
			log.Info("udpReader: ReadFrom: error %v", err)
			break
		}

		// make a copy because we will overwrite buf
		b := make([]byte, n)
		copy(b, buf)

		var srcIP net.IP
		src := b[20:24]
		srcIP = src

		log.Info("udpReader:", "recv bytes", n, "interface", ifname, "source service IP", net.ParseIP(srcIP.String()))

		dstIP := net.ParseIP("127.0.0.1")

		h := &ipv4.Header{
			Version:  ipv4.Version,
			Len:      ipv4.HeaderLen,
			TotalLen: ipv4.HeaderLen + len(b),
			ID:       12345,
			Protocol: 1,
			TTL:      1,
			Dst:      dstIP.To4(),
		}

		c, err := net.ListenPacket("ip4:icmp", "0.0.0.0")
		if err != nil {
			klog.Errorf("Unable to create connection: %v", err)
		}
		defer c.Close()

		p, err := ipv4.NewRawConn(c)
		if err != nil {
			klog.Errorf("Unable to open new raw connection: %v", err)
		}

		err = p.WriteTo(h, b, nil)
		if err != nil {
			klog.Warningf("unable to send bytes: %v %d", err)
			break
		}

	}

	log.Info("udpReader: exiting '%s'", ifname)

}
