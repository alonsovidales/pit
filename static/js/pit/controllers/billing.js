var BillingController = (function() {
	function round(num) {
		return Math.round(num * 1000) / 1000
	}

	var my = {
		init: function() {
			$.ajax({
				type: 'POST',
				dataType: 'json',
				url: cAPIBase + '/billing_info',
				data: {
					u: LoginController.getUser(),
					k: LoginController.getKey(),
				},
				success: function(data) {
					var billingTable = $('#billing-pending-table-body');
					console.log('Result');
					console.log(data);
					console.log(data.history);
					var i = 0;
					$.each(data.history.reverse(), function(_, line) {
						billingTable.append($("\
							<tr>\
								<th scope=\"row\">" + i++ + "</th>\
								<td>" + line.group + "</td>\
								<td>" + line.instances + "</td>\
								<td>" + line.type + "</td>\
								<td>" + new Date(line.from * 1000) + "</td>\
								<td>" + new Date(line.to * 1000) + "</td>\
								<td>" + round((line.to - line.from) / 3600) + "</td>\
								<td>" + round(line.price) + "</td>\
							</tr>\
						"));
					});
				},
				error: function() {
					alert("The billing information can't be read");
				},
			});
		}
	};

	return my;
})();
