package io.github.maas3n.mattmux

import android.app.Activity
import android.content.Context
import com.android.billingclient.api.AcknowledgePurchaseParams
import com.android.billingclient.api.BillingClient
import com.android.billingclient.api.BillingClientStateListener
import com.android.billingclient.api.BillingFlowParams
import com.android.billingclient.api.BillingResult
import com.android.billingclient.api.PendingPurchasesParams
import com.android.billingclient.api.ProductDetails
import com.android.billingclient.api.Purchase
import com.android.billingclient.api.PurchasesUpdatedListener
import com.android.billingclient.api.QueryProductDetailsParams
import com.android.billingclient.api.QueryPurchasesParams

class BillingManager(
    context: Context,
    private val listener: Listener,
) : PurchasesUpdatedListener {

    interface Listener {
        fun onBillingState(state: State)
    }

    data class State(
        val connected: Boolean = false,
        val proOwned: Boolean = false,
        val price: String? = null,
        val message: String? = null,
    )

    companion object {
        const val PRO_PRODUCT_ID = "mattrip_pro"
    }

    private var productDetails: ProductDetails? = null
    private var state = State(message = "Connecting to Google Play…")

    private val billingClient = BillingClient.newBuilder(context.applicationContext)
        .setListener(this)
        .enablePendingPurchases(
            PendingPurchasesParams.newBuilder()
                .enableOneTimeProducts()
                .build()
        )
        .enableAutoServiceReconnection()
        .build()

    fun start() {
        if (billingClient.isReady) {
            refresh()
            return
        }

        billingClient.startConnection(object : BillingClientStateListener {
            override fun onBillingSetupFinished(result: BillingResult) {
                if (result.responseCode == BillingClient.BillingResponseCode.OK) {
                    update(state.copy(connected = true, message = null))
                    refresh()
                } else {
                    update(
                        state.copy(
                            connected = false,
                            message = "Google Play Billing unavailable (${result.responseCode}).",
                        )
                    )
                }
            }

            override fun onBillingServiceDisconnected() {
                update(state.copy(connected = false, message = "Google Play Billing disconnected."))
            }
        })
    }

    fun close() {
        billingClient.endConnection()
    }

    fun launchProPurchase(activity: Activity): BillingResult? {
        if (!BuildConfig.ENABLE_BILLING_PURCHASES) {
            update(state.copy(message = "Purchases stay disabled until the Android remux engine is ready."))
            return null
        }

        val details = productDetails ?: run {
            update(state.copy(message = "MattRip Pro is not available from Google Play yet."))
            return null
        }

        val offerToken = details.oneTimePurchaseOfferDetailsList
            ?.firstOrNull()
            ?.offerToken
        val productParamsBuilder = BillingFlowParams.ProductDetailsParams.newBuilder()
            .setProductDetails(details)
        if (!offerToken.isNullOrEmpty()) {
            productParamsBuilder.setOfferToken(offerToken)
        }

        val params = BillingFlowParams.newBuilder()
            .setProductDetailsParamsList(listOf(productParamsBuilder.build()))
            .build()

        return billingClient.launchBillingFlow(activity, params)
    }

    private fun refresh() {
        queryProduct()
        queryPurchases()
    }

    private fun queryProduct() {
        val product = QueryProductDetailsParams.Product.newBuilder()
            .setProductId(PRO_PRODUCT_ID)
            .setProductType(BillingClient.ProductType.INAPP)
            .build()
        val params = QueryProductDetailsParams.newBuilder()
            .setProductList(listOf(product))
            .build()

        billingClient.queryProductDetailsAsync(params) { result, detailsResult ->
            if (result.responseCode != BillingClient.BillingResponseCode.OK) {
                update(state.copy(message = "Could not load MattRip Pro pricing."))
                return@queryProductDetailsAsync
            }

            productDetails = detailsResult.productDetailsList
                .firstOrNull { it.productId == PRO_PRODUCT_ID }
            val price = productDetails
                ?.oneTimePurchaseOfferDetailsList
                ?.firstOrNull()
                ?.formattedPrice
            update(state.copy(price = price))
        }
    }

    private fun queryPurchases() {
        val params = QueryPurchasesParams.newBuilder()
            .setProductType(BillingClient.ProductType.INAPP)
            .build()
        billingClient.queryPurchasesAsync(params) { result, purchases ->
            if (result.responseCode == BillingClient.BillingResponseCode.OK) {
                processPurchases(purchases)
            }
        }
    }

    override fun onPurchasesUpdated(result: BillingResult, purchases: List<Purchase>?) {
        when (result.responseCode) {
            BillingClient.BillingResponseCode.OK -> processPurchases(purchases.orEmpty())
            BillingClient.BillingResponseCode.USER_CANCELED -> update(state.copy(message = "Purchase canceled."))
            BillingClient.BillingResponseCode.ITEM_ALREADY_OWNED -> queryPurchases()
            else -> update(state.copy(message = "Purchase failed (${result.responseCode})."))
        }
    }

    private fun processPurchases(purchases: List<Purchase>) {
        var ownsPro = false

        for (purchase in purchases) {
            if (PRO_PRODUCT_ID !in purchase.products) continue
            when (purchase.purchaseState) {
                Purchase.PurchaseState.PURCHASED -> {
                    ownsPro = true
                    if (!purchase.isAcknowledged) {
                        acknowledge(purchase)
                    }
                }
                Purchase.PurchaseState.PENDING -> {
                    update(state.copy(message = "MattRip Pro purchase is pending."))
                }
            }
        }

        update(
            state.copy(
                proOwned = ownsPro,
                message = if (ownsPro) "MattRip Pro unlocked." else state.message,
            )
        )
    }

    private fun acknowledge(purchase: Purchase) {
        val params = AcknowledgePurchaseParams.newBuilder()
            .setPurchaseToken(purchase.purchaseToken)
            .build()
        billingClient.acknowledgePurchase(params) { result ->
            if (result.responseCode != BillingClient.BillingResponseCode.OK) {
                update(state.copy(message = "Purchase received, but acknowledgement is still pending."))
            }
        }
    }

    private fun update(newState: State) {
        state = newState
        listener.onBillingState(newState)
    }
}
