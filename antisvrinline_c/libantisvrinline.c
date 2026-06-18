#define _GNU_SOURCE
#include <sys/types.h>
#include <sys/mman.h>
#include <dlfcn.h>
#include <errno.h>
#include <pthread.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

typedef int (*GCEnvironmentFunc)(void);
typedef int (*GetTcpPortFunc)(unsigned int p1);
typedef int* (*GetAntiBotInterfaceAllFunc)(void);

#define EXT_SERVER_MSG 80

static pthread_mutex_t g_initMutex = PTHREAD_MUTEX_INITIALIZER;
static int g_inlineInitialized = 0;
static unsigned int g_jmpSendChatMsgAddr = 0;

static void exec_code_patch(void* addr, const unsigned char* data, int len)
{
	int pagesize;
	uint8_t* mem;
	void* page;

	pagesize = sysconf(_SC_PAGE_SIZE);
	if (pagesize == -1)
		exit(errno);

	mem = (uint8_t*)addr;
	page = (uint8_t*)(mem - ((uint32_t)(uintptr_t)mem % (uint32_t)pagesize));
	if (mprotect(page, (size_t)pagesize, PROT_READ | PROT_WRITE | PROT_EXEC))
		exit(errno);

	memcpy(mem, data, (size_t)len);

	if (mprotect(page, (size_t)pagesize, PROT_EXEC))
		exit(errno);
}

static void set_hook_x86(void* target, void* replacement, unsigned int* jumpAddr, int offset)
{
	uint8_t machineCode[] = {
		0xb8, 0x00, 0x00, 0x00, 0x00,
		0xff, 0xe0
	};
	int pagesize;
	uint8_t* mem;
	void* page;

	pagesize = sysconf(_SC_PAGE_SIZE);
	if (pagesize == -1)
		exit(errno);

	mem = (uint8_t*)target;
	page = (uint8_t*)(mem - ((uint32_t)(uintptr_t)mem % (uint32_t)pagesize));
	if (mprotect(page, (size_t)pagesize, PROT_READ | PROT_WRITE | PROT_EXEC))
		exit(errno);

	memcpy(machineCode + 1, &replacement, sizeof(replacement));
	memcpy(mem, machineCode, sizeof(machineCode));
	*jumpAddr = (unsigned int)(uintptr_t)target + (unsigned int)offset;

	if (mprotect(page, (size_t)pagesize, PROT_EXEC))
		exit(errno);
}

__attribute__((__naked__))
static int _SendChatMsg(unsigned int p1, unsigned int p2, unsigned int p3,
                        unsigned int p4, unsigned int p5, unsigned int p6,
                        unsigned int p7, unsigned int p8, unsigned int p9,
                        unsigned int p10)
{
	__asm__ __volatile__ (
		"push %%edi\n\t"
		"push %%esi\n\t"
		"push %%ebx\n\t"
		"sub $540, %%esp\n\t"
		"movl %0, %%eax\n\t"
		"jmp *%%eax"
		:
		: "m"(g_jmpSendChatMsgAddr)
	);
}

static int MySendChatMsg(unsigned int p1, unsigned int p2, unsigned int p3,
                         unsigned int p4, unsigned int p5, unsigned int p6,
                         unsigned int p7, unsigned int p8, unsigned int p9,
                         unsigned int p10)
{
	if (p3 == EXT_SERVER_MSG) {
		p3 = 11;
		p4 = 0;
		p5 = 0;
	}
	return _SendChatMsg(p1, p2, p3, p4, p5, p6, p7, p8, p9, p10);
}

static void InstallChatMessageHook(FILE* log)
{
	unsigned int sendChatMsgProcessAddr = 0x086C975E;

	set_hook_x86((void*)(uintptr_t)sendChatMsgProcessAddr, (void*)MySendChatMsg, &g_jmpSendChatMsgAddr, 12);
	if (log != NULL)
		fprintf(log, "INFO: SendChatMsg hook enabled.\n");
}

static void InstallAntibotRecvBypass(FILE* log)
{
	unsigned int patchAddr = 0x085949A0;
	unsigned char patch[5] = { 0xB8, 0x00, 0x00, 0x00, 0x00 };

	exec_code_patch((void*)(uintptr_t)patchAddr, patch, 5);
	if (log != NULL)
		fprintf(log, "INFO: Antibot recv bypass patched at 0x%08X\n", patchAddr);
}

static void InstallAntibotInputBypass(FILE* log)
{
	unsigned int patchAddr = 0x080EDDB0;
	unsigned char patch[6] = { 0xC3, 0x90, 0x90, 0x90, 0x90, 0x90 };

	exec_code_patch((void*)(uintptr_t)patchAddr, patch, 6);
	if (log != NULL)
		fprintf(log, "INFO: Antibot input bypass patched at 0x%08X\n", patchAddr);
}

static void InstallIPGQueryBypass(FILE* log)
{
	unsigned int patchAddr = 0x08100790;
	unsigned char patch[6] = { 0x31, 0xC0, 0xC3, 0x90, 0x90, 0x90 };

	exec_code_patch((void*)(uintptr_t)patchAddr, patch, 6);
	if (log != NULL)
		fprintf(log, "INFO: IPGQuery bypass patched at 0x%08X\n", patchAddr);
}

static FILE* openInlineLog(void)
{
	char path[128];
	int port;
	FILE* log;

	port = ((GetTcpPortFunc)(0x0857F428))(((GCEnvironmentFunc)(0x080CC181))());
	snprintf(path, sizeof(path), "useronline-%d.log", port);

	log = fopen(path, "ab");
	if (log == NULL)
		log = fopen("/dev/null", "w");
	return log;
}

static int* loadRealAntiBotInterface(FILE* log)
{
	void* handle;
	GetAntiBotInterfaceAllFunc realGetAntiBotInterfaceAll;

	handle = dlopen("./libantisvrimport.so.orig", RTLD_NOW);
	if (handle == NULL)
		handle = dlopen("./libantisvrimport.so", RTLD_NOW);
	if (handle == NULL) {
		const char* err = dlerror();
		printf("libantisvrimport.so dlopen failed!\n");
		if (err != NULL)
			printf("%s\n", err);
		if (log != NULL)
			fprintf(log, "ERROR: libantisvrimport.so dlopen failed: %s\n", err != NULL ? err : "");
		return NULL;
	}

	realGetAntiBotInterfaceAll = (GetAntiBotInterfaceAllFunc)dlsym(handle, "GetAntiBotInterfaceAll");
	if (realGetAntiBotInterfaceAll == NULL) {
		const char* err = dlerror();
		printf("libantisvrimport.so dlsym failed!\n");
		if (err != NULL)
			printf("%s\n", err);
		if (log != NULL)
			fprintf(log, "ERROR: libantisvrimport.so dlsym failed: %s\n", err != NULL ? err : "");
		return NULL;
	}

	return realGetAntiBotInterfaceAll();
}

static int initInlineOnce(FILE* log)
{
	pthread_mutex_lock(&g_initMutex);
	if (g_inlineInitialized) {
		pthread_mutex_unlock(&g_initMutex);
		if (log != NULL)
			fprintf(log, "INFO: already initialized.\n");
		return 0;
	}
	g_inlineInitialized = 1;
	pthread_mutex_unlock(&g_initMutex);

	InstallChatMessageHook(log);
	InstallAntibotRecvBypass(log);
	InstallAntibotInputBypass(log);
	InstallIPGQueryBypass(log);

	if (log != NULL)
		fprintf(log, "INFO: proxy init success.\n");
	return 1;
}

__attribute__((visibility("default")))
int* GetAntiBotInterfaceAll(void)
{
	FILE* log;
	int* realInterface;

	log = openInlineLog();
	if (log == NULL)
		return NULL;

	realInterface = loadRealAntiBotInterface(log);
	if (realInterface != NULL)
		initInlineOnce(log);

	fclose(log);
	return realInterface;
}
