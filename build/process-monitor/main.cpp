#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#define _WIN32_DCOM

#include <windows.h>
#include <comdef.h>
#include <Wbemidl.h>
#include <iostream>
#include <string>
#include <thread>
#include <atomic>
#include <mutex>
#include <queue>
#include <memory>

#pragma comment(lib, "wbemuuid.lib")
#pragma comment(lib, "ole32.lib")
#pragma comment(lib, "oleaut32.lib")

#ifdef WIN32
    #define EXPORT __declspec(dllexport)
#else
    #define EXPORT
#endif

// Event structure
struct ProcessEvent {
    bool isCreation; // true for creation, false for deletion
    std::string processName;
    DWORD pid;
};

// Event callback type
typedef void (*EventCallback)(bool isCreation, const char* processName, DWORD pid);

class ProcessMonitor {
private:
    IWbemLocator* pLoc = nullptr;
    IWbemServices* pSvc = nullptr;
    IUnsecuredApartment* pUnsecApp = nullptr;
    IWbemObjectSink* pCreationSink = nullptr;
    IWbemObjectSink* pDeletionSink = nullptr;
    
    std::atomic<bool> running{false};
    std::thread monitorThread;
    
    EventCallback callback = nullptr;
    std::mutex callbackMutex;

    bool watchCreation = false;
    bool watchDeletion = false;

public:
    ProcessMonitor() {}
    
    ~ProcessMonitor() {
        Stop();
    }

    bool Initialize(bool creation, bool deletion, EventCallback cb) {
        if (running) return false;

        watchCreation = creation;
        watchDeletion = deletion;
        callback = cb;

        HRESULT hres;

        // Initialize COM (accept S_FALSE if already initialized)
        hres = CoInitializeEx(0, COINIT_MULTITHREADED);
        if (FAILED(hres) && hres != RPC_E_CHANGED_MODE) {
            // If COM is already initialized with a different threading model, that's OK too
            return false;
        }

        // Set COM security levels (may already be set in Electron)
        hres = CoInitializeSecurity(
            NULL,
            -1,
            NULL,
            NULL,
            RPC_C_AUTHN_LEVEL_DEFAULT,
            RPC_C_IMP_LEVEL_IMPERSONATE,
            NULL,
            EOAC_NONE,
            NULL
        );

        // COM security might already be initialized - that's fine
        if (FAILED(hres) && hres != RPC_E_TOO_LATE) {
            // Don't fail here, security might already be set
        }

        // Obtain the initial locator to WMI
        hres = CoCreateInstance(
            CLSID_WbemLocator,
            0,
            CLSCTX_INPROC_SERVER,
            IID_IWbemLocator,
            (LPVOID*)&pLoc
        );

        if (FAILED(hres)) {
            CoUninitialize();
            return false;
        }

        // Connect to WMI
        hres = pLoc->ConnectServer(
            _bstr_t(L"ROOT\\CIMV2"),
            NULL,
            NULL,
            0,
            NULL,
            0,
            0,
            &pSvc
        );

        if (FAILED(hres)) {
            pLoc->Release();
            CoUninitialize();
            return false;
        }

        // Set security levels on the proxy
        hres = CoSetProxyBlanket(
            pSvc,
            RPC_C_AUTHN_WINNT,
            RPC_C_AUTHZ_NONE,
            NULL,
            RPC_C_AUTHN_LEVEL_CALL,
            RPC_C_IMP_LEVEL_IMPERSONATE,
            NULL,
            EOAC_NONE
        );

        if (FAILED(hres)) {
            pSvc->Release();
            pLoc->Release();
            CoUninitialize();
            return false;
        }

        // Create event sinks
        if (watchCreation) {
            if (!CreateEventSink(true)) {
                Cleanup();
                return false;
            }
        }

        if (watchDeletion) {
            if (!CreateEventSink(false)) {
                Cleanup();
                return false;
            }
        }

        running = true;
        return true;
    }

    void Stop() {
        if (!running) return;
        
        running = false;
        
        if (monitorThread.joinable()) {
            monitorThread.join();
        }

        Cleanup();
    }

private:
    bool CreateEventSink(bool isCreation) {
        HRESULT hres;

        // Query for events
        const wchar_t* query = isCreation 
            ? L"SELECT * FROM __InstanceCreationEvent WITHIN 1 WHERE TargetInstance ISA 'Win32_Process'"
            : L"SELECT * FROM __InstanceDeletionEvent WITHIN 1 WHERE TargetInstance ISA 'Win32_Process'";

        IWbemObjectSink** ppSink = isCreation ? &pCreationSink : &pDeletionSink;

        // Create the event sink
        EventSink* pSink = new EventSink(this, isCreation);
        pSink->AddRef();

        hres = pSvc->ExecNotificationQueryAsync(
            _bstr_t("WQL"),
            _bstr_t(query),
            WBEM_FLAG_SEND_STATUS,
            NULL,
            pSink
        );

        if (FAILED(hres)) {
            pSink->Release();
            return false;
        }

        *ppSink = pSink;
        return true;
    }

    void Cleanup() {
        if (pCreationSink) {
            pSvc->CancelAsyncCall(pCreationSink);
            pCreationSink->Release();
            pCreationSink = nullptr;
        }

        if (pDeletionSink) {
            pSvc->CancelAsyncCall(pDeletionSink);
            pDeletionSink->Release();
            pDeletionSink = nullptr;
        }

        if (pSvc) {
            pSvc->Release();
            pSvc = nullptr;
        }

        if (pLoc) {
            pLoc->Release();
            pLoc = nullptr;
        }

        CoUninitialize();
    }

    // Event sink class
    class EventSink : public IWbemObjectSink {
    private:
        LONG m_lRef;
        ProcessMonitor* monitor;
        bool isCreation;

    public:
        EventSink(ProcessMonitor* mon, bool creation) 
            : m_lRef(0), monitor(mon), isCreation(creation) {}
        ~EventSink() {}

        virtual ULONG STDMETHODCALLTYPE AddRef() {
            return InterlockedIncrement(&m_lRef);
        }

        virtual ULONG STDMETHODCALLTYPE Release() {
            LONG lRef = InterlockedDecrement(&m_lRef);
            if (lRef == 0)
                delete this;
            return lRef;
        }

        virtual HRESULT STDMETHODCALLTYPE QueryInterface(REFIID riid, void** ppv) {
            if (riid == IID_IUnknown || riid == IID_IWbemObjectSink) {
                *ppv = (IWbemObjectSink*)this;
                AddRef();
                return WBEM_S_NO_ERROR;
            }
            return E_NOINTERFACE;
        }

        virtual HRESULT STDMETHODCALLTYPE Indicate(
            LONG lObjectCount,
            IWbemClassObject** apObjArray
        ) {
            for (int i = 0; i < lObjectCount; i++) {
                IWbemClassObject* pObj = apObjArray[i];
                VARIANT vtProp;
                HRESULT hr;

                // Get the TargetInstance property
                hr = pObj->Get(_bstr_t(L"TargetInstance"), 0, &vtProp, 0, 0);
                if (SUCCEEDED(hr) && vtProp.vt == VT_UNKNOWN) {
                    IWbemClassObject* pTargetInstance = nullptr;
                    hr = vtProp.punkVal->QueryInterface(IID_IWbemClassObject, (void**)&pTargetInstance);
                    
                    if (SUCCEEDED(hr)) {
                        // Get process name
                        VARIANT vtName;
                        hr = pTargetInstance->Get(_bstr_t(L"Name"), 0, &vtName, 0, 0);
                        
                        // Get process ID
                        VARIANT vtPid;
                        pTargetInstance->Get(_bstr_t(L"ProcessId"), 0, &vtPid, 0, 0);

                        if (SUCCEEDED(hr)) {
                            std::string processName = _com_util::ConvertBSTRToString(vtName.bstrVal);
                            DWORD pid = vtPid.uintVal;

                            // Call the callback
                            if (monitor->callback) {
                                std::lock_guard<std::mutex> lock(monitor->callbackMutex);
                                monitor->callback(isCreation, processName.c_str(), pid);
                            }

                            VariantClear(&vtName);
                            VariantClear(&vtPid);
                        }

                        pTargetInstance->Release();
                    }
                }
                VariantClear(&vtProp);
            }

            return WBEM_S_NO_ERROR;
        }

        virtual HRESULT STDMETHODCALLTYPE SetStatus(
            LONG lFlags,
            HRESULT hResult,
            BSTR strParam,
            IWbemClassObject* pObjParam
        ) {
            return WBEM_S_NO_ERROR;
        }
    };

    friend class EventSink;
};

// Global monitor instance
static ProcessMonitor* g_monitor = nullptr;
static std::mutex g_monitorMutex;

extern "C" {
    // Start monitoring with callback
    EXPORT bool StartMonitoring(bool watchCreation, bool watchDeletion, EventCallback callback) {
        std::lock_guard<std::mutex> lock(g_monitorMutex);
        
        if (g_monitor) {
            return false; // Already running
        }

        g_monitor = new ProcessMonitor();
        if (!g_monitor->Initialize(watchCreation, watchDeletion, callback)) {
            delete g_monitor;
            g_monitor = nullptr;
            return false;
        }

        return true;
    }

    // Stop monitoring
    EXPORT void StopMonitoring() {
        std::lock_guard<std::mutex> lock(g_monitorMutex);
        
        if (g_monitor) {
            g_monitor->Stop();
            delete g_monitor;
            g_monitor = nullptr;
        }
    }

    // Check if monitoring is active
    EXPORT bool IsMonitoring() {
        std::lock_guard<std::mutex> lock(g_monitorMutex);
        return g_monitor != nullptr;
    }
}
