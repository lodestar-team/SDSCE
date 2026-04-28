package devenv

import "github.com/streamingfast/eth-go"

const (
	DefaultDeployerPrivateKeyHex        = "1aa5d8f9a42ba0b9439c7034d24e93619f67af22a9ab15be9e4ce7eadddb5143"
	DefaultServiceProviderPrivateKeyHex = "41942233cf1d78b6e3262f1806f8da36aafa24a941031aad8e056a1d34640f8d"
	DefaultPayerPrivateKeyHex           = "e4c2694501255921b6588519cfd36d4e86ddc4ce19ab1bc91d9c58057c040304"
	DefaultUser1PrivateKeyHex           = "dd02564c0e9836fb570322be23f8355761d4d04ebccdc53f4f53325227680a9f"
	DefaultUser2PrivateKeyHex           = "bc3def46fab7929038dfb0df7e0168cba60d3384aceabf85e23e5e0ff90c8fe3"
	DefaultUser3PrivateKeyHex           = "7acd0f26d5be968f73ca8f2198fa52cc595650f8d5819ee9122fe90329847c48"
	DefaultDemoSignerPrivateKeyHex      = "0bba7d355d1750fce9756af7887e826e8071a56d9d8e327f546b1f34c78f9281"

	DefaultGraphTallyCollectorAddressHex = "0x1d01649b4f94722b55b5c3b3e10fe26cd90c1ba9"
	DefaultPaymentsEscrowAddressHex      = "0xfc7487a37ca8eac2e64cba61277aa109e9b8631e"
	DefaultSubstreamsDataServiceHex      = "0x37478fd2f5845e3664fe4155d74c00e1a4e7a5e2"
	DefaultMockGRTTokenAddressHex        = "0xfa7a048544f86c11206afd89b40bc987e464cb58"
	DefaultMockStakingAddressHex         = "0x32f01bc7a55d437b7a8354621a9486b9be08a3bb"
)

type DeterministicAccounts struct {
	Deployer        Account
	ServiceProvider Account
	Payer           Account
	User1           Account
	User2           Account
	User3           Account
	DemoSigner      Account
}

type DeterministicContracts struct {
	Collector   eth.Address
	Escrow      eth.Address
	DataService eth.Address
	GRTToken    eth.Address
	Staking     eth.Address
}

func DefaultAccounts() DeterministicAccounts {
	return DeterministicAccounts{
		Deployer:        mustAccountFromHex(DefaultDeployerPrivateKeyHex),
		ServiceProvider: mustAccountFromHex(DefaultServiceProviderPrivateKeyHex),
		Payer:           mustAccountFromHex(DefaultPayerPrivateKeyHex),
		User1:           mustAccountFromHex(DefaultUser1PrivateKeyHex),
		User2:           mustAccountFromHex(DefaultUser2PrivateKeyHex),
		User3:           mustAccountFromHex(DefaultUser3PrivateKeyHex),
		DemoSigner:      mustAccountFromHex(DefaultDemoSignerPrivateKeyHex),
	}
}

func DefaultContractAddresses() DeterministicContracts {
	return DeterministicContracts{
		Collector:   eth.MustNewAddress(DefaultGraphTallyCollectorAddressHex),
		Escrow:      eth.MustNewAddress(DefaultPaymentsEscrowAddressHex),
		DataService: eth.MustNewAddress(DefaultSubstreamsDataServiceHex),
		GRTToken:    eth.MustNewAddress(DefaultMockGRTTokenAddressHex),
		Staking:     eth.MustNewAddress(DefaultMockStakingAddressHex),
	}
}
