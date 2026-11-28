package org.netmerge.android

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import org.json.JSONObject
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/** Credentials stay encrypted on this device; never include these values in logs or intents. */
internal class SavedVpnProfile(val link: String, val endpointIp: String) {
    override fun toString() = "SavedVpnProfile(redacted)"
}

internal object VpnProfileStore {
    private const val ALIAS = "netmerge.experimental.profile.v1"
    private const val PREFS = "experimental_vpn_profile"
    private const val ENTRY = "encrypted_v1"
    private val aad = "netmerge-profile-v1".toByteArray(Charsets.UTF_8)

    private fun key(create: Boolean): SecretKey {
        val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        val existing = ks.getKey(ALIAS, null) as? SecretKey
        if (existing != null) return existing
        check(create) { "VPN profile key unavailable" }
        return KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore").apply {
            init(
                KeyGenParameterSpec.Builder(ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                    .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                    .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                    .setKeySize(256)
                    .setRandomizedEncryptionRequired(true)
                    .build()
            )
        }.generateKey()
    }

    private fun prefs(context: Context) =
        context.applicationContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    @Synchronized
    fun save(context: Context, profile: SavedVpnProfile) {
        require(profile.link.length in 1..16384 && profile.link.startsWith("vless://"))
        require(profile.endpointIp.length in 1..64)
        // Only a basic UI guard; native validation must run before a VPN starts.
        val plain = JSONObject().put("link", profile.link).put("endpointIp", profile.endpointIp)
            .toString().toByteArray(Charsets.UTF_8)
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        try {
            cipher.init(Cipher.ENCRYPT_MODE, key(true))
            cipher.updateAAD(aad)
            val nonce = cipher.iv
            check(nonce.size == 12)
            val encrypted = cipher.doFinal(plain)
            val blob = byteArrayOf(1) + nonce + encrypted
            check(prefs(context).edit().putString(ENTRY, Base64.encodeToString(blob, Base64.NO_WRAP)).commit())
        } finally {
            plain.fill(0)
        }
    }

    @Synchronized
    fun load(context: Context): SavedVpnProfile? {
        val encoded = prefs(context).getString(ENTRY, null) ?: return null
        check(encoded.length <= 100000)
        val blob = Base64.decode(encoded, Base64.NO_WRAP)
        check(blob.size >= 29 && blob[0] == 1.toByte())
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, key(false), GCMParameterSpec(128, blob.copyOfRange(1, 13)))
        cipher.updateAAD(aad)
        val plain = cipher.doFinal(blob.copyOfRange(13, blob.size))
        try {
            val obj = JSONObject(String(plain, Charsets.UTF_8))
            return SavedVpnProfile(obj.getString("link"), obj.getString("endpointIp"))
        } finally {
            plain.fill(0)
        }
    }

    @Synchronized
    fun clear(context: Context) {
        check(prefs(context).edit().remove(ENTRY).commit())
        KeyStore.getInstance("AndroidKeyStore").apply { load(null) }.deleteEntry(ALIAS)
    }
}
