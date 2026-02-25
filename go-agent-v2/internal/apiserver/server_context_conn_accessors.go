package apiserver

func notifyHookFuncState(s *Server) func(method string, params any) {
	if s == nil {
		return nil
	}
	return s.notifyHookState.hook()
}

func snapshotSSEClientsState(s *Server) []chan []byte {
	if s == nil {
		return nil
	}
	return s.sseState.snapshotClients()
}

func connsSnapshotState(s *Server) map[string]*connEntry {
	if s == nil {
		return nil
	}
	return s.connManagerState.connsSnapshot()
}

func removeConnState(s *Server, connID string) (*connEntry, bool) {
	if s == nil {
		return nil, false
	}
	return s.connManagerState.removeConn(connID)
}

func allocPendingRequestState(s *Server) (reqID int64, ch <-chan *Response, cleanup func()) {
	if s == nil {
		return 0, nil, func() {}
	}
	return s.connManagerState.allocPendingRequest()
}

func getConnState(s *Server, connID string) (*connEntry, bool) {
	if s == nil {
		return nil, false
	}
	return s.connManagerState.getConn(connID)
}

func firstConnIDState(s *Server) string {
	if s == nil {
		return ""
	}
	return s.connManagerState.firstConnID()
}

func deliverPendingResponseState(s *Server, reqID int64, resp *Response) (bool, bool) {
	if s == nil {
		return false, false
	}
	return s.connManagerState.deliverPendingResponse(reqID, resp)
}

func connectionCountState(s *Server) int {
	if s == nil {
		return 0
	}
	return s.connManagerState.connectionCount()
}

func allocConnIDState(s *Server) string {
	if s == nil {
		return ""
	}
	return s.connManagerState.allocConnID()
}

func addConnState(s *Server, connID string, entry *connEntry) {
	if s == nil {
		return
	}
	s.connManagerState.addConn(connID, entry)
}
