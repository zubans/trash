package com.healthlogin.app;

import android.os.Bundle;
import com.getcapacitor.BridgeActivity;
import com.healthlogin.app.plugins.UpdatePlugin;

public class MainActivity extends BridgeActivity {
    @Override
    public void onCreate(Bundle savedInstanceState) {
        registerPlugin(UpdatePlugin.class);
        super.onCreate(savedInstanceState);
    }
}
