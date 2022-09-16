package logging

//go:generate sh -c "mockgen -package logging -self_package github.com/fkwhite/quic-goV2.0/logging -destination mock_connection_tracer_test.go github.com/fkwhite/quic-goV2.0/logging ConnectionTracer"
//go:generate sh -c "mockgen -package logging -self_package github.com/fkwhite/quic-goV2.0/logging -destination mock_tracer_test.go github.com/fkwhite/quic-goV2.0/logging Tracer"
