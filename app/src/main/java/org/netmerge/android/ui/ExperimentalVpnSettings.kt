package org.netmerge.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.ui.draw.clip
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.netmerge.android.SavedVpnProfile
import org.netmerge.android.VpnProfileStore
import org.netmerge.android.VpnAppExclusions

/** Draft settings UI; not connected to the original direct-mode activity. */
@Composable
internal fun ExperimentalVpnSettings(
    running: Boolean,
    profilePage: Boolean,
    validate: suspend (String, String) -> Boolean,
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current.applicationContext
    val scope = rememberCoroutineScope()
    // Do not use rememberSaveable: secrets must not enter saved activity state.
    var link by remember { mutableStateOf("") }
    var endpoint by remember { mutableStateOf("") }
    var saved by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var ready by remember { mutableStateOf(false) }
    var search by remember { mutableStateOf("") }
    val palette = LocalPalette.current
    var status by remember { mutableStateOf("") }
    var apps by remember { mutableStateOf(emptyList<VpnAppExclusions.App>()) }
    var selected by remember { mutableStateOf(emptySet<String>()) }
    LaunchedEffect(context) {
        val loaded = withContext(Dispatchers.IO) {
            Triple(runCatching { VpnProfileStore.load(context) != null }, VpnAppExclusions.visibleApps(context), VpnAppExclusions.load(context))
        }
        saved = loaded.first.getOrDefault(false)
        if (loaded.first.isFailure) status = "Stored profile could not be opened. Replace or delete it."
        apps = loaded.second
        selected = loaded.third
        ready = true
    }
    val editable = ready && !running && !busy
    Column(modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        if (profilePage) {
        Text("VPN profile", style = MaterialTheme.typography.headlineSmall)
        Text("Saving a profile does not start or verify a VPN connection.")
        Text(if (saved) "Encrypted profile saved" else "No saved profile")
        OutlinedTextField(
            value = link,
            onValueChange = { if (it.length <= 16384) link = it },
            label = { Text("VLESS share link") },
            visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
            singleLine = true,
            enabled = editable,
            modifier = Modifier.fillMaxWidth(),
        )
        OutlinedTextField(
            value = endpoint,
            onValueChange = { if (it.length <= 64) endpoint = it },
            label = { Text("VPN endpoint IP") },
            singleLine = true,
            enabled = editable,
            modifier = Modifier.fillMaxWidth(),
        )
        Row {
            Button(enabled = editable && link.isNotBlank() && endpoint.isNotBlank(), onClick = {
                busy = true
                val profile = SavedVpnProfile(link.trim(), endpoint.trim())
                scope.launch {
                    try {
                        val valid = withContext(Dispatchers.IO) { validate(profile.link, profile.endpointIp) }
                        if (!valid) {
                            status = "Invalid or unsupported VPN configuration"
                        } else {
                            withContext(Dispatchers.IO) { VpnProfileStore.save(context, profile) }
                            saved = true
                            link = ""
                            endpoint = ""
                            status = "Profile saved locally"
                        }
                    } catch (_: Exception) {
                        status = "Could not save VPN profile"
                    } finally { busy = false }
                }
            }) { Text("Save profile") }
            TextButton(enabled = editable, onClick = {
                busy = true
                scope.launch {
                    try {
                        withContext(Dispatchers.IO) { VpnProfileStore.clear(context) }
                        saved = false
                        link = ""
                        endpoint = ""
                        status = "Profile deleted"
                    } catch (_: Exception) {
                        status = "Could not delete profile"
                    } finally { busy = false }
                }
            }) { Text("Delete") }
        }
        } else {
        Text("App exclusions", style = MaterialTheme.typography.headlineSmall)
        Text("Excluded: "+selected.size+" · Changes save automatically", color = palette.muted)
        OutlinedTextField(value = search, onValueChange = { search = it }, label = { Text("Search apps") }, singleLine = true, modifier = Modifier.fillMaxWidth())
        Text("Selected apps deliberately bypass VPN protection. Changes require restarting the VPN.")
        if (apps.isEmpty()) Text("No launcher apps found")
        for (app in apps.filter { it.label.contains(search,true) || it.packageName.contains(search,true) }) {
            Row(Modifier.fillMaxWidth().clip(RoundedCornerShape(14.dp)).background(palette.card).padding(8.dp), verticalAlignment = Alignment.CenterVertically) {
                Checkbox(checked = app.packageName in selected, enabled = editable, onCheckedChange = { checked ->
                    try {
                        // Commit immediately before leaving the screen. No hidden Save button.
                        selected = VpnAppExclusions.setExcluded(context,app.packageName,checked)
                        status = "App exclusions saved"
                    } catch (_: Exception) {
                        selected = VpnAppExclusions.load(context)
                        status = "Could not save exclusions"
                    }
                })
                Column {
                    Text(app.label,color = MaterialTheme.colorScheme.onSurface)
                    Text(app.packageName,color = palette.muted,style = MaterialTheme.typography.bodySmall)
                }
            }
        }
        }
        if (status.isNotEmpty()) Text(status)
    }
}
