package dev.dronelink.bridge

import android.app.Application
import android.content.Context
import com.cySdkyc.clx.Helper

class BridgeApp : Application() {
    override fun attachBaseContext(base: Context?) {
        super.attachBaseContext(base)
        Helper.install(this)          // required by MSDK V5 (see DJI's sample DJIAircraftApplication)
    }

    override fun onCreate() {
        super.onCreate()
        DjiAdapter.init(this)         // SDKManager.init -> registerApp, as in DJI's sample
    }
}
