var BillingController = (function() {
	var my = {
		init: function() {
			$.ajax({
				type: 'POST',
				url: cAPIBase + '/billing_info',
				data: {
					u: LoginController.getUser(),
					k: LoginController.getKey(),
				},
				success: function(data) {
					console.log('Result');
					console.log(data);
				},
				error: function() {
					alert("The billing information can't be read");
				},
			});
		}
	};

	return my;
})();
