package catalogue

var handlerRegistry = map[NativeHandlerID]handlerInfo{
	HandlerSSHForward:           {allowedForLocal: true, destructivePlan: false},
	HandlerSSHForwardList:       {allowedForLocal: true, destructivePlan: false},
	HandlerSSHForwardStop:       {allowedForLocal: true, destructivePlan: true},
	HandlerEnvShow:              {allowedForLocal: true, destructivePlan: false},
	HandlerEnvList:              {allowedForLocal: true, destructivePlan: false},
	HandlerEnvGenerateSecretHex: {allowedForLocal: true, destructivePlan: false},
	HandlerEnvExport:            {allowedForLocal: true, destructivePlan: false},
	HandlerPortKill:             {allowedForLocal: false, destructivePlan: true},
	HandlerProcessKill:          {allowedForLocal: false, destructivePlan: true},
	HandlerFsTarGz:              {allowedForLocal: false, destructivePlan: true},
	HandlerFsUnzip:              {allowedForLocal: false, destructivePlan: true},
	HandlerSCPToRemote:          {allowedForLocal: false, destructivePlan: false},
	HandlerSCPFromRemote:        {allowedForLocal: false, destructivePlan: false},
	HandlerSSHFixPermissions:    {allowedForLocal: false, destructivePlan: true},
	HandlerSSHPemPermissions:    {allowedForLocal: false, destructivePlan: true},
	HandlerGitCleanLocalGone:    {allowedForLocal: false, destructivePlan: true},
	HandlerGitForceCleanGone:    {allowedForLocal: false, destructivePlan: true},
	HandlerGitCleanMerged:       {allowedForLocal: false, destructivePlan: true},
}

type handlerInfo struct {
	allowedForLocal bool
	destructivePlan bool
}

func HandlerRegistered(id NativeHandlerID) bool {
	_, ok := handlerRegistry[id]
	return ok
}

func HandlerAllowedForSource(id NativeHandlerID, source Source) bool {
	info, ok := handlerRegistry[id]
	if !ok {
		return false
	}
	if source == SourceLocal {
		return info.allowedForLocal
	}
	return true
}

func HandlerHasDestructivePlan(id NativeHandlerID) bool {
	info, ok := handlerRegistry[id]
	return ok && info.destructivePlan
}
